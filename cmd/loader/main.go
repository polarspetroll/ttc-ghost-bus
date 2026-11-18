package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/polarspetroll/ttc-ghost-bus/internal/gtfs"
	sqlfiles "github.com/polarspetroll/ttc-ghost-bus/sql"
)

func main() {
	file := flag.String("file", "", "local gtfs zip (skips download)")
	url := flag.String("url", "", "gtfs zip url (default: ask toronto open data)")
	days := flag.Int("days", 7, "how many days ahead to build scheduled_trips")
	from := flag.String("from", "", "first service date to build, YYYY-MM-DD (default today)")
	types := flag.String("route-types", "0,3", "gtfs route types to keep, 0 streetcar 3 bus. subway is not in the bus feed")
	force := flag.Bool("force", false, "reload static tables even if zip did not change")
	flag.Parse()

	ctx := context.Background()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(ctx, sqlfiles.Schema); err != nil {
		log.Fatal(err)
	}

	data, src, err := getZip(ctx, *file, *url)
	if err != nil {
		log.Fatal("get gtfs: ", err)
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	var seen bool
	db.QueryRow(ctx, "select exists(select 1 from gtfs_versions where sha256=$1)", hash).Scan(&seen)

	tz, _ := time.LoadLocation("America/Toronto")
	start := time.Now().In(tz)
	if *from != "" {
		start, err = time.ParseInLocation("2006-01-02", *from, tz)
		if err != nil {
			log.Fatal(err)
		}
	}
	startDay := start.Format("2006-01-02")

	if seen && !*force {
		log.Println("gtfs unchanged, only extending schedule")
	} else {
		log.Printf("new gtfs %s from %s, loading", hash[:12], src)
		if err := loadStatic(ctx, db, data, hash, src, *types); err != nil {
			log.Fatal("load: ", err)
		}
		// new board period, rebuild future days with new trips
		if _, err := db.Exec(ctx, "delete from scheduled_trips where service_date >= $1::date", startDay); err != nil {
			log.Fatal(err)
		}
	}

	end := start.AddDate(0, 0, *days-1).Format("2006-01-02")
	tag, err := db.Exec(ctx, buildSQL, startDay, end)
	if err != nil {
		log.Fatal("build schedule: ", err)
	}
	log.Printf("scheduled_trips %s to %s: %d new rows", startDay, end, tag.RowsAffected())
}

func getZip(ctx context.Context, file, url string) ([]byte, string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		return b, file, err
	}
	c := &http.Client{Timeout: 5 * time.Minute}
	if url == "" {
		var err error
		url, err = gtfs.ResolveZipURL(ctx, c)
		if err != nil {
			return nil, "", err
		}
	}
	b, err := gtfs.Download(ctx, c, url)
	return b, url, err
}

func loadStatic(ctx context.Context, db *pgxpool.Pool, data []byte, hash, src, types string) error {
	f, err := gtfs.Open(data)
	if err != nil {
		return err
	}

	keepType := map[string]bool{}
	for _, t := range strings.Split(types, ",") {
		keepType[strings.TrimSpace(t)] = true
	}

	routes := [][]any{}
	keepRoute := map[string]bool{}
	err = f.Each("routes.txt", func(r map[string]string) error {
		if !keepType[r["route_type"]] {
			return nil
		}
		rt, _ := strconv.Atoi(r["route_type"])
		keepRoute[r["route_id"]] = true
		routes = append(routes, []any{r["route_id"], r["route_short_name"], r["route_long_name"], rt})
		return nil
	})
	if err != nil {
		return err
	}

	trips := [][]any{}
	keepTrip := map[string]bool{}
	err = f.Each("trips.txt", func(r map[string]string) error {
		if !keepRoute[r["route_id"]] {
			return nil
		}
		keepTrip[r["trip_id"]] = true
		var dir *int16
		if d, err := strconv.Atoi(r["direction_id"]); err == nil {
			x := int16(d)
			dir = &x
		}
		trips = append(trips, []any{r["trip_id"], r["route_id"], r["service_id"], dir})
		return nil
	})
	if err != nil {
		return err
	}

	// first and last time of each trip,stop_times is huge so just keep min max
	span := map[string][2]int{}
	err = f.Each("stop_times.txt", func(r map[string]string) error {
		id := r["trip_id"]
		if !keepTrip[id] {
			return nil
		}
		dep, arr := r["departure_time"], r["arrival_time"]
		if dep == "" {
			dep = arr
		}
		if arr == "" {
			arr = dep
		}
		if dep == "" {
			return nil // untimed stop
		}
		d, err := gtfs.ParseTime(dep)
		if err != nil {
			return err
		}
		a, err := gtfs.ParseTime(arr)
		if err != nil {
			return err
		}
		s, ok := span[id]
		if !ok {
			span[id] = [2]int{d, a}
			return nil
		}
		if d < s[0] {
			s[0] = d
		}
		if a > s[1] {
			s[1] = a
		}
		span[id] = s
		return nil
	})
	if err != nil {
		return err
	}
	times := make([][]any, 0, len(span))
	for id, s := range span {
		times = append(times, []any{id, s[0], s[1]})
	}

	cal := [][]any{}
	if f.Has("calendar.txt") {
		err = f.Each("calendar.txt", func(r map[string]string) error {
			days := ""
			for _, d := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
				if r[d] == "1" {
					days += "1"
				} else {
					days += "0"
				}
			}
			s, e1 := gtfsDate(r["start_date"])
			e, e2 := gtfsDate(r["end_date"])
			if e1 != nil || e2 != nil {
				return fmt.Errorf("calendar dates for %s", r["service_id"])
			}
			cal = append(cal, []any{r["service_id"], days, s, e})
			return nil
		})
		if err != nil {
			return err
		}
	}

	exc := [][]any{}
	if f.Has("calendar_dates.txt") {
		err = f.Each("calendar_dates.txt", func(r map[string]string) error {
			d, err := gtfsDate(r["date"])
			if err != nil {
				return err
			}
			t, _ := strconv.Atoi(r["exception_type"])
			exc = append(exc, []any{r["service_id"], d, int16(t)})
			return nil
		})
		if err != nil {
			return err
		}
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "truncate routes, static_trips, trip_times, calendar, calendar_dates"); err != nil {
		return err
	}
	copies := []struct {
		table string
		cols  []string
		rows  [][]any
	}{
		{"routes", []string{"route_id", "short_name", "long_name", "route_type"}, routes},
		{"static_trips", []string{"trip_id", "route_id", "service_id", "direction_id"}, trips},
		{"trip_times", []string{"trip_id", "start_sec", "end_sec"}, times},
		{"calendar", []string{"service_id", "days", "start_date", "end_date"}, cal},
		{"calendar_dates", []string{"service_id", "date", "exception_type"}, exc},
	}
	for _, c := range copies {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{c.table}, c.cols, pgx.CopyFromRows(c.rows)); err != nil {
			return fmt.Errorf("copy %s: %w", c.table, err)
		}
		log.Printf("  %s: %d rows", c.table, len(c.rows))
	}
	if _, err := tx.Exec(ctx, "insert into gtfs_versions (sha256, source) values ($1,$2) on conflict do nothing", hash, src); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func gtfsDate(s string) (time.Time, error)  {
	return time.Parse("20060102", s)
}

// service day starts at noon minus 12h (handles dst properly), then add the gtfs seconds
const buildSQL = `
insert into scheduled_trips (service_date, trip_id, route_id, direction_id, start_ts, end_ts)
select d.day, t.trip_id, t.route_id, t.direction_id,
       base + make_interval(secs => tt.start_sec),
       base + make_interval(secs => tt.end_sec)
from (
  select g::date as day,
         (g::date + time '12:00') at time zone 'America/Toronto' - interval '12 hours' as base
  from generate_series($1::date, $2::date, interval '1 day') g
) d
join static_trips t on true
join trip_times tt on tt.trip_id = t.trip_id
where (
    exists (select 1 from calendar c
            where c.service_id = t.service_id
              and d.day between c.start_date and c.end_date
              and substr(c.days, extract(isodow from d.day)::int, 1) = '1')
    and not exists (select 1 from calendar_dates x
                    where x.service_id = t.service_id and x.date = d.day and x.exception_type = 2)
  )
  or exists (select 1 from calendar_dates x
             where x.service_id = t.service_id and x.date = d.day and x.exception_type = 1)
on conflict do nothing`
