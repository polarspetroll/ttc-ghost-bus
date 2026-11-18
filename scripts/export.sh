#!/usr/bin/env bash
# dumps the chart data to csv then draws the charts. needs DATABASE_URL
set -euo pipefail
mkdir -p docs/data

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
set timezone = 'America/Toronto';
\copy (select c.route_id, coalesce(r.short_name, c.route_id) as name, count(*) as trips, sum((n_positions = 0)::int) as ghosts from trip_coverage c left join routes r using (route_id) where not excluded and extract(isodow from service_date) < 6 group by 1, 2 order by ghosts desc) to 'docs/data/ghosts_by_route.csv' csv header
\copy (select date_trunc('week', service_date)::date as week_of, count(*) as trips, sum((n_positions = 0)::int) as ghosts from trip_coverage where not excluded and extract(isodow from service_date) < 6 group by 1 order by 1) to 'docs/data/weekly.csv' csv header
SQL

python3 scripts/charts.py
