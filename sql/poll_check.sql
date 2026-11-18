-- weekly health check. psql "$DATABASE_URL" -f sql/poll_check.sql
set timezone = 'America/Toronto';

select polled_at::date as day,
       count(*) as attempts,
       count(*) filter (where ok) as ok,
       round(100.0 * count(*) filter (where ok) / count(*), 2) as success_pct,
       sum(n_inserted) as positions_inserted,
       round(avg(duration_ms)) as avg_ms
from poll_log
where polled_at > now() - interval '7 days'
group by 1
order by 1;

-- most common errors
select left(error, 80) as error, count(*)
from poll_log
where not ok and polled_at > now() - interval '7 days'
group by 1
order by 2 desc
limit 5;
