// feed_check samples the live feed and tells us how many vehicles carry a trip_id that exists in static trips.txt
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/polarspetroll/ttc-ghost-bus/internal/feed"
	"github.com/polarspetroll/ttc-ghost-bus/internal/gtfs"
)

func main() {
	n := flag.Int("n", 50, "number of snapshots")
	every := flag.Duration("every", 30*time.Second, "time between snapshots")
	url := flag.String("url", feed.DefaultURL, "vehicle positions url")
	zipFile := flag.String("gtfs", "", "static gtfs zip (default download from open data)")
	flag.Parse()

	ctx := context.Background()
	c := &http.Client{Timeout: 5 * time.Minute}

	data, err := loadZip(ctx, c, *zipFile)
	if err != nil {
		log.Fatal("static gtfs: ", err)
	}
	f, err := gtfs.Open(data)
	if err != nil {
		log.Fatal(err)
	}
	static := map[string]bool{}
	f.Each("trips.txt", func(r map[string]string) error {
		static[r["trip_id"]] = true
		return nil
	})
	log.Printf("%d trips in static trips.txt", len(static))

	total, withTrip, matched, ok := 0, 0, 0, 0
	unmatched := map[string]int{}
	for i := 0; i < *n; i++ {
		if i > 0 {
			time.Sleep(*every)
		}
		msg, _, err := feed.Fetch(ctx, c, *url)
		if err != nil {
			log.Printf("snapshot %d failed: %v", i+1, err)
			continue
		}
		ok++
		for _, v := range feed.Vehicles(msg) {
			total++
			if v.TripID == "" {
				continue
			}
			withTrip++
			if static[v.TripID] {
				matched++
			} else if len(unmatched) < 20 {
				unmatched[v.TripID]++
			}
		}
		log.Printf("snapshot %d/%d ok", i+1, *n)
	}
	if total == 0 {
		log.Fatal("no vehicles seen, cant compute match rate")
	}

	rate := 100 * float64(matched) / float64(total)
	fmt.Println("## Feed check")
	fmt.Printf("- run at: %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("- snapshots ok: %d of %d\n", ok, *n)
	fmt.Printf("- vehicle pings: %d, with trip_id: %d, trip_id in trips.txt: %d\n", total, withTrip, matched)
	fmt.Printf("- **trip match rate: %.2f%%**\n", rate)
	if rate < 90 {
		fmt.Println("- below 90%: fallback match (route + direction + nearest scheduled start) is needed")
	} else {
		fmt.Println("- 90% or more: direct trip_id match is enough, fallback only catches leftovers")
	}
	if len(unmatched) > 0 {
		ex := []string{}
		for id := range unmatched {
			ex = append(ex, id)
		}
		fmt.Printf("- example unmatched trip ids: %s\n", strings.Join(ex, ", "))
	}

	// do trip updates and alerts exist on the same host?
	base := strings.TrimSuffix(*url, "/vehicles")
	fmt.Println("\n## Other feeds on the same host")
	for _, path := range []string{"/trips", "/alerts"} {
		_, status, err := feed.Fetch(ctx, c, base+path)
		res := "ok, decodes as gtfs-rt"
		if err != nil {
			res = err.Error()
		}
		fmt.Printf("- %s%s: status %d, %s\n", base, path, status, res)
	}
}

func loadZip(ctx context.Context, c *http.Client, file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	u, err := gtfs.ResolveZipURL(ctx, c)
	if err != nil {
		return nil, err
	}
	return gtfs.Download(ctx, c, u)
}
