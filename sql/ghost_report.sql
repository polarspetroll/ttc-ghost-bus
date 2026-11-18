-- ghost report. psql "$DATABASE_URL" -f sql/ghost_report.sql
-- a ghost = scheduled trip with zero positions in its window, trips that touch a poller downtime hour are left out
set timezone = 'America/Toronto';

create temp view counted as
select c.*,
       c.n_positions = 0 as ghost,
       extract(isodow from c.service_date) < 6 as weekday
from trip_coverage c
where not c.excluded;

\echo '== headline =='
select
  (select min(service_date) from counted) as first_day,
  (select max(service_date) from counted) as last_day,
  (select count(distinct service_date) from counted) as days,
  (select round(100.0 * avg(ghost::int), 2) from counted where weekday) as weekday_ghost_pct,
  (select round(100.0 * avg(ghost::int), 2) from counted where not weekday) as weekend_ghost_pct,
  (select round(100.0 * sum(n_ok) / nullif(sum(n_polls), 0), 2) from daily_stats) as poll_success_pct,
  (select round(avg(n_positions)) from daily_stats where n_positions > 0) as avg_positions_per_day;

\echo '== ghost rate by day =='
select service_date, to_char(service_date, 'Dy') as dow,
       count(*) as trips, sum(ghost::int) as ghosts,
       round(100.0 * avg(ghost::int), 2) as ghost_pct
from counted
group by service_date
order by service_date;

\echo '== weekday vs weekend =='
select case when weekday then 'weekday' else 'weekend' end as day_type,
       count(*) as trips, sum(ghost::int) as ghosts,
       round(100.0 * avg(ghost::int), 2) as ghost_pct
from counted
group by weekday
order by weekday desc;

\echo '== weekly weekday ghost rate =='
select date_trunc('week', service_date)::date as week_of,
       count(*) as trips, sum(ghost::int) as ghosts,
       round(100.0 * avg(ghost::int), 2) as ghost_pct
from counted
where weekday
group by 1
order by 1;

\echo '== routes ranked by ghost count (weekdays) =='
select c.route_id, r.short_name, r.long_name,
       count(*) as trips, sum(ghost::int) as ghosts,
       round(100.0 * avg(ghost::int), 2) as ghost_pct
from counted c
left join routes r on r.route_id = c.route_id
where weekday
group by c.route_id, r.short_name, r.long_name
having sum(ghost::int) > 0
order by ghosts desc, c.route_id
limit 30;

\echo '== smallest set of routes holding most (over half) of weekday ghosts =='
with per_route as (
  select route_id, sum(ghost::int) as ghosts
  from counted
  where weekday
  group by route_id
  having sum(ghost::int) > 0
),
ranked as (
  select route_id, ghosts,
         sum(ghosts) over (order by ghosts desc, route_id) as running,
         sum(ghosts) over () as total,
         row_number() over (order by ghosts desc, route_id) as rank
  from per_route
)
select rank, route_id, ghosts, running,
       round(100.0 * running / total, 1) as running_pct
from ranked
where running - ghosts < total * 0.5
order by rank;
