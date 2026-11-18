-- safe to run many times, poller runs it on startup

create table if not exists poll_log (
  id           bigserial primary key,
  polled_at    timestamptz not null default now(),
  ok           boolean not null,
  http_status  int,
  n_vehicles   int,
  n_inserted   int,
  feed_ts      timestamptz,
  duration_ms  int,
  error        text
);
create index if not exists poll_log_at on poll_log (polled_at);

-- raw pings, one partition per toronto day
create table if not exists positions (
  vehicle_id   text not null,
  trip_id      text,
  route_id     text,
  direction_id smallint,
  lat          double precision,
  lon          double precision,
  feed_ts      timestamptz not null,
  received_ts  timestamptz not null
) partition by range (received_ts);
create index if not exists positions_trip_ts on positions (trip_id, feed_ts);

create or replace function ensure_partition(d date) returns void
language plpgsql as $$
declare
  tname text := 'positions_' || to_char(d, 'YYYYMMDD');
begin
  execute format(
    'create table if not exists %I partition of positions for values from (%L) to (%L)',
    tname,
    (d::timestamp at time zone 'America/Toronto'),
    ((d + 1)::timestamp at time zone 'America/Toronto'));
end $$;

-- drops raw partitions older than keep_days
create or replace function drop_old_partitions(keep_days int) returns int
language plpgsql as $$
declare
  r record;
  n int := 0;
  cutoff text := 'positions_' || to_char((now() at time zone 'America/Toronto')::date - keep_days, 'YYYYMMDD');
begin
  for r in
    select c.relname from pg_inherits i
    join pg_class c on c.oid = i.inhrelid
    join pg_class p on p.oid = i.inhparent
    where p.relname = 'positions' and c.relname < cutoff
  loop
    execute format('drop table %I', r.relname);
    n := n + 1;
  end loop;
  return n;
end $$;


-- static gtfs, replaced on each new board period
create table if not exists gtfs_versions (
  sha256     text primary key,
  source     text,
  loaded_at  timestamptz not null default now()
);

create table if not exists routes (
  route_id    text primary key,
  short_name  text,
  long_name   text,
  route_type  int
);

create table if not exists static_trips (
  trip_id      text primary key,
  route_id     text not null,
  service_id   text not null,
  direction_id smallint
);

create table if not exists trip_times (
  trip_id    text primary key,
  start_sec  int not null,
  end_sec    int not null
);

-- days is 7 chars mon..sun like 1111100
create table if not exists calendar (
  service_id  text primary key,
  days        char(7) not null,
  start_date  date not null,
  end_date    date not null
);

create table if not exists calendar_dates (
  service_id      text not null,
  date            date not null,
  exception_type  smallint not null,
  primary key (service_id, date)
);

create table if not exists scheduled_trips (
  service_date  date not null,
  trip_id       text not null,
  route_id      text not null,
  direction_id  smallint,
  start_ts      timestamptz not null,
  end_ts        timestamptz not null,
  primary key (service_date, trip_id)
);
create index if not exists scheduled_route_start on scheduled_trips (route_id, start_ts);


-- built nightly
create table if not exists trip_coverage (
  service_date  date not null,
  trip_id       text not null,
  route_id      text not null,
  start_ts      timestamptz not null,
  end_ts        timestamptz not null,
  first_seen    timestamptz,
  last_seen     timestamptz,
  n_positions   int not null default 0,
  excluded      boolean not null default false,
  primary key (service_date, trip_id)
);

create table if not exists fallback_matches (
  service_date  date not null,
  trip_id       text not null,
  vehicle_id    text not null,
  raw_trip_id   text not null,
  first_seen    timestamptz not null,
  last_seen     timestamptz not null,
  n_positions   int not null,
  primary key (vehicle_id, raw_trip_id, first_seen)
);

create table if not exists downtime_hours (
  hour_start  timestamptz primary key,
  n_ok        int not null
);

create table if not exists daily_stats (
  day          date primary key,
  n_positions  bigint not null,
  n_polls      int not null,
  n_ok         int not null
);
