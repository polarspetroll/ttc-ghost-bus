package feed

import (
	"testing"
	"time"

	"github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"google.golang.org/protobuf/proto"
)

func sample() *gtfs.FeedMessage {
	return &gtfs.FeedMessage{
		Header: &gtfs.FeedHeader{GtfsRealtimeVersion: proto.String("2.0"), Timestamp: proto.Uint64(1700000000)},
		Entity: []*gtfs.FeedEntity{
			{Id: proto.String("1"), Vehicle: &gtfs.VehiclePosition{
				Vehicle:   &gtfs.VehicleDescriptor{Id: proto.String("8001")},
				Trip:      &gtfs.TripDescriptor{TripId: proto.String("T1"), RouteId: proto.String("29"), DirectionId: proto.Uint32(1)},
				Position:  &gtfs.Position{Latitude: proto.Float32(43.7), Longitude: proto.Float32(-79.4)},
				Timestamp: proto.Uint64(1700000010),
			}},
			// no position, should be skipped
			{Id: proto.String("2"), Vehicle: &gtfs.VehiclePosition{Vehicle: &gtfs.VehicleDescriptor{Id: proto.String("8002")}}},
			{Id: proto.String("3"), Vehicle: &gtfs.VehiclePosition{
				Vehicle:  &gtfs.VehicleDescriptor{Id: proto.String("8003")},
				Position: &gtfs.Position{Latitude: proto.Float32(43.6), Longitude: proto.Float32(-79.3)},
			}},
		},
	}
}

func TestVehicles(t *testing.T) {
	vs := Vehicles(sample())
	if len(vs) != 2 {
		t.Fatalf("want 2 vehicles got %d", len(vs))
	}
	if vs[0].TripID != "T1" || vs[0].RouteID != "29" || *vs[0].DirectionID != 1 {
		t.Errorf("bad first vehicle %+v", vs[0])
	}
	// falls back to header ts
	if vs[1].FeedTs.Unix() != 1700000000 || vs[1].DirectionID != nil {
		t.Errorf("bad second vehicle %+v", vs[1])
	}
}

func TestDedup(t *testing.T) {
	d := NewDedup()
	ts := time.Unix(100, 0)
	a := []Vehicle{{VehicleID: "a", FeedTs: ts}, {VehicleID: "b", FeedTs: ts}}
	if n := len(d.Filter(a)); n != 2 {
		t.Fatalf("first pass want 2 got %d", n)
	}
	b := []Vehicle{{VehicleID: "a", FeedTs: ts}, {VehicleID: "b", FeedTs: ts.Add(30 * time.Second)}}
	got := d.Filter(b)
	if len(got) != 1 || got[0].VehicleID != "b" {
		t.Fatalf("second pass wrong %+v", got)
	}
}

func TestRoundTrip(t *testing.T) {
	raw, _ := proto.Marshal(sample())
	m := &gtfs.FeedMessage{}
	if err := proto.Unmarshal(raw, m); err != nil {
		t.Fatal(err)
	}
	if len(Vehicles(m)) != 2 {
		t.Fatal("roundtrip lost vehicles")
	}
}
