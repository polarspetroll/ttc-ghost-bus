-- nightly job. rebuilds the last 3 service days so a missed night fixes itself
-- run with: psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f sql/aggregate.sql
set timezone = 'America/Toronto';

begin;

-- an hour is "down" if we got less than 90% of the 120 polls we expect
insert into downtime_hours (hour_start, n_ok)
select h, coalesce(x.n, 0)
from generate_series(date_trunc('hour', now() - interval '4 days'),
                     date_trunc('hour', now()) - interval '1 hour',
                     interval '1 hour') h
left join (
  select date_trunc('hour', polled_at) hr, count(*) filter (where ok) n
  from poll_log
  where polled_at > now() - interval '5 days'
  group by 1
) x on x.hr = h
where coalesce(x.n, 0) < 108
on conflict (hour_start) do update set n_ok = excluded.n_ok;


-- fallback: vehicles whose trip_id is not in static gtfs get matched on route + direction + nearest start
insert into fallback_matches (service_date, trip_id, vehicle_id, raw_trip_id, first_seen, last_seen, n_positions)
select s.service_date, s.trip_id, g.vehicle_id, g.raw_trip_id, g.f, g.l, g.n
from (
  -- group per day too, (local time - 4h) so late night trips stay with their day. same trip id can come back tomorrow
  select vehicle_id, trip_id raw_trip_id, route_id, direction_id,
         min(feed_ts) f, max(feed_ts) l, count(*) n
  from positions p
  where p.received_ts > now() - interval '4 days'
    and p.trip_id is not null
    and p.route_id is not null
    and not exists (select 1 from static_trips t where t.trip_id = p.trip_id)
  group by 1, 2, 3, 4, (p.feed_ts - interval '4 hours')::date
) g
cross join lateral (
  select service_date, trip_id
  from scheduled_trips s
  where s.route_id = g.route_id
    and (g.direction_id is null or s.direction_id = g.direction_id)
    and s.start_ts between g.f - interval '15 minutes' and g.f + interval '15 minutes'
  order by abs(extract(epoch from s.start_ts - g.f))
  limit 1
) s
on conflict (vehicle_id, raw_trip_id, first_seen) do nothing;


-- coverage per scheduled trip. window is 5 min before first stop to last stop
with direct as (
  select s.service_date, s.trip_id, min(p.feed_ts) f, max(p.feed_ts) l, count(*) n
  from scheduled_trips s
  join positions p on p.trip_id = s.trip_id
                  and p.feed_ts between s.start_ts - interval '5 minutes' and s.end_ts
  where s.service_date between current_date - 3 and current_date - 1
  group by 1, 2
),
fb as (
  select service_date, trip_id, min(first_seen) f, max(last_seen) l, sum(n_positions)::bigint n
  from fallback_matches
  where service_date between current_date - 3 and current_date - 1
  group by 1, 2
),
seen as (
  select * from direct union all select * from fb
)
insert into trip_coverage (service_date, trip_id, route_id, start_ts, end_ts, first_seen, last_seen, n_positions, excluded)
select s.service_date, s.trip_id, s.route_id, s.start_ts, s.end_ts,
       min(b.f), max(b.l), coalesce(sum(b.n), 0),
       exists (select 1 from downtime_hours d
               where d.hour_start < s.end_ts
                 and d.hour_start + interval '1 hour' > s.start_ts - interval '5 minutes')
from scheduled_trips s
left join seen b on b.service_date = s.service_date and b.trip_id = s.trip_id
where s.service_date between current_date - 3 and current_date - 1
group by s.service_date, s.trip_id, s.route_id, s.start_ts, s.end_ts
on conflict (service_date, trip_id) do update set
  first_seen  = excluded.first_seen,
  last_seen   = excluded.last_seen,
  n_positions = excluded.n_positions,
  excluded    = excluded.excluded;


-- daily counters, raw positions get deleted later so save the count now
insert into daily_stats (day, n_positions, n_polls, n_ok)
select d::date,
       (select count(*) from positions where received_ts >= d and received_ts < d + interval '1 day'),
       (select count(*) from poll_log where polled_at >= d and polled_at < d + interval '1 day'),
       (select count(*) from poll_log where ok and polled_at >= d and polled_at < d + interval '1 day')
from generate_series(current_date - 3, current_date - 1, interval '1 day') d
on conflict (day) do update set
  n_positions = excluded.n_positions,
  n_polls     = excluded.n_polls,
  n_ok        = excluded.n_ok;

commit;

-- keep 7 days of raw positions
select drop_old_partitions(7) as dropped_partitions;
