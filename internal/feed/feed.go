package feed

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

const DefaultURL = "https://bustime.ttc.ca/gtfsrt/vehicles"

type Vehicle struct {
	VehicleID   string
	TripID      string
	RouteID     string
	DirectionID *int16
	Lat, Lon    float64
	FeedTs      time.Time
}

// Fetch downloads and decodes one snapshot. status is the http code (0 if request failed)
func Fetch(ctx context.Context, c *http.Client, url string) (*gtfs.FeedMessage, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("feed returned %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	msg := &gtfs.FeedMessage{}
	if err := proto.Unmarshal(body, msg); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode: %w", err)
	}
	return msg, resp.StatusCode, nil
}

// Vehicles pulls out the vehicle positions we care about, entities without id or position are dropped
func Vehicles(msg *gtfs.FeedMessage) []Vehicle {
	headerTs := int64(msg.GetHeader().GetTimestamp())
	out := []Vehicle{}

	for _, e := range msg.GetEntity() {
		v := e.GetVehicle()
		if v == nil || v.GetPosition() == nil {
			continue
		}
		id := v.GetVehicle().GetId()
		if id == "" {
			id = e.GetId()
		}
		if id == "" {
			continue
		}

		ts := int64(v.GetTimestamp())
		if ts == 0 {
			ts = headerTs
		}
		var dir *int16
		if v.GetTrip() != nil && v.GetTrip().DirectionId != nil {
			d := int16(v.GetTrip().GetDirectionId())
			dir = &d
		}

		out = append(out, Vehicle{
			VehicleID:   id,
			TripID:      v.GetTrip().GetTripId(),
			RouteID:     v.GetTrip().GetRouteId(),
			DirectionID: dir,
			Lat:         float64(v.GetPosition().GetLatitude()),
			Lon:         float64(v.GetPosition().GetLongitude()),
			FeedTs:      time.Unix(ts, 0).UTC(),
		})
	}
	return out
}

// Dedup remembers last timestamp per vehicle so we dont store same ping twice
type Dedup struct {
	last map[string]time.Time
}

func NewDedup() *Dedup { return &Dedup{last: map[string]time.Time{}} }

func (d *Dedup) Filter(vs []Vehicle) []Vehicle {
	keep := []Vehicle{} 
	for _, v := range vs {
		if t, ok := d.last[v.VehicleID]; ok && t.Equal(v.FeedTs) {
			continue
		}
		d.last[v.VehicleID] = v.FeedTs
		keep = append(keep, v)
	}
	return keep
}
