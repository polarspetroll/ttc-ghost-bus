package gtfs

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseTime(t *testing.T) {
	cases := map[string]int{
		"00:00:00": 0,
		"05:30:15": 5*3600 + 30*60 + 15,
		"25:10:00": 25*3600 + 600,
		" 7:05:00": 7*3600 + 300,
	}
	for in, want := range cases {
		got, err := ParseTime(in)
		if err != nil || got != want {
			t.Errorf("ParseTime(%q) = %d, %v want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "12:00", "aa:00:00", "10:61:00"} {
		if _, err := ParseTime(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestEach(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("gtfs/trips.txt")
	w.Write([]byte("\ufeffroute_id,service_id,trip_id\n29,wk,T1\n7,wk,T2\n"))
	zw.Close()

	f, err := Open(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	err = f.Each("trips.txt", func(r map[string]string) error {
		ids = append(ids, r["trip_id"]+"/"+r["route_id"])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "T1/29" {
		t.Fatalf("got %v", ids)
	}
	if f.Has("stops.txt") {
		t.Error("stops.txt should not exist")
	}
}
