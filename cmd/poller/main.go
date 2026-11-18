package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/polarspetroll/ttc-ghost-bus/internal/feed"
	sqlfiles "github.com/polarspetroll/ttc-ghost-bus/sql"
)

var toronto, _ = time.LoadLocation("America/Toronto")

type poller struct {
	db       *pgxpool.Pool
	client   *http.Client
	url      string
	dedup *feed.Dedup
	lastPart string
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set")
	}
	url := os.Getenv("FEED_URL")
	if url == "" {
		url = feed.DefaultURL
	}
	interval := 30 * time.Second
	if v, err := time.ParseDuration(os.Getenv("POLL_INTERVAL")); err == nil {
		interval = v
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// db might not be up yet when the pod starts
	for i := 0; ; i++ {
		_, err = db.Exec(ctx, sqlfiles.Schema)
		if err == nil {
			break
		}
		if i > 20 || ctx.Err() != nil {
			log.Fatal("schema: ", err)
		}
		log.Println("waiting for db:", err)
		time.Sleep(5 * time.Second)
	}

	p := &poller{
		db:     db,
		client: &http.Client{Timeout: 15 * time.Second},
		url:    url,
		dedup:  feed.NewDedup(),
	}
	log.Printf("polling %s every %s", url, interval)
	p.run(ctx, interval)
}

func (p *poller) run(ctx context.Context, interval time.Duration) {
	fails := 0

	for {
		start := time.Now()
		err := p.pollOnce(ctx)

		wait := interval - time.Since(start)
		if err != nil {
			fails++
			// 5s 10s 20s ... up to 5 min
			wait = 5 * time.Second << min(fails-1, 6)
			if wait > 5*time.Minute {
				wait = 5 * time.Minute
			}
			log.Printf("poll failed (%d in a row), retry in %s: %v", fails, wait, err)
		} else {
			fails = 0
		}

		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return
		case <-time.After(wait):
		}
	}
}

func (p *poller) pollOnce(ctx context.Context) error {
	start := time.Now()
	entry := logRow{}

	err := func() error {
		msg, status, err := feed.Fetch(ctx, p.client, p.url)
		entry.status = status
		if err != nil {
			return err
		}
		if h := msg.GetHeader().GetTimestamp(); h > 0 {
			t := time.Unix(int64(h), 0)
			entry.feedTs = &t
		}

		all := feed.Vehicles(msg)
		fresh := p.dedup.Filter(all)
		entry.nVehicles = len(all)

		n, err := p.insert(ctx, fresh)
		entry.nInserted = n
		return err
	}()

	entry.ok = err == nil
	entry.ms = int(time.Since(start).Milliseconds())
	if err != nil {
		entry.err = err.Error()
	}
	if lerr := p.writeLog(ctx, entry); lerr != nil {
		log.Println("poll_log insert failed:", lerr)
	}
	return err
}


func (p *poller) insert(ctx context.Context, vs []feed.Vehicle) (int, error) {
	if len(vs) == 0 {
		return 0, nil
	}
	now := time.Now()
	day := now.In(toronto).Format("2006-01-02")
	if day != p.lastPart {
		// make today and tomorrow partition so midnight dont fail
		for _, d := range []time.Time{now, now.Add(24 * time.Hour)} {
			if _, err := p.db.Exec(ctx, "select ensure_partition($1::date)", d.In(toronto).Format("2006-01-02")); err != nil {
				return 0, err
			}
		}
		p.lastPart = day
	}

	rows := make([][]any, 0, len(vs))
	for _, v := range vs {
		rows = append(rows, []any{v.VehicleID, nullable(v.TripID), nullable(v.RouteID), v.DirectionID, v.Lat, v.Lon, v.FeedTs, now})
	}
	n, err := p.db.CopyFrom(ctx, pgx.Identifier{"positions"},
		[]string{"vehicle_id", "trip_id", "route_id", "direction_id", "lat", "lon", "feed_ts", "received_ts"},
		pgx.CopyFromRows(rows))
	return int(n), err
}

type logRow struct {
	ok                   bool
	status               int
	nVehicles, nInserted int
	feedTs               *time.Time
	ms                   int
	err                  string
}

func (p *poller) writeLog(ctx context.Context, e logRow) error {
	_, err := p.db.Exec(ctx,
		`insert into poll_log (ok, http_status, n_vehicles, n_inserted, feed_ts, duration_ms, error)
		 values ($1, $2, $3, $4, $5, $6, $7)`,
		e.ok, e.status, e.nVehicles, e.nInserted, e.feedTs, e.ms, nullable(e.err))
	return err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

