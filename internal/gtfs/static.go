package gtfs

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// toronto open data package for the ttc schedule
const CKANPackage = "https://ckan0.cf.opendata.inter.prod-toronto.ca/api/3/action/package_show?id=merged-gtfs-ttc-routes-and-schedules"

// ParseTime turns "25:10:30" into seconds after service day start. gtfs allows hours past 24
func ParseTime(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("bad time %q", s)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	sec, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil || m > 59 || sec > 59 {
		return 0, fmt.Errorf("bad time %q", s)
	}
	return h*3600 + m*60 + sec, nil
}

// ResolveZipURL asks ckan for the zip resource of the gtfs dataset
func ResolveZipURL(ctx context.Context, c *http.Client) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", CKANPackage, nil)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var body struct {
		Result struct {
			Resources []struct {
				URL    string `json:"url"`
				Format string `json:"format"`
			} `json:"resources"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	for _, r := range body.Result.Resources {
		if strings.EqualFold(r.Format, "zip") || strings.HasSuffix(strings.ToLower(r.URL), ".zip") {
			return r.URL, nil
		}
	}
	return "", fmt.Errorf("no zip resource found in ckan package")
}

func Download(ctx context.Context, c *http.Client, url string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// Feed is an opened gtfs zip
type Feed struct {
	z *zip.Reader
}

func Open(data []byte) (*Feed, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	return &Feed{z: z}, nil
}


// Each calls fn for every row of a csv file in the zip, row is keyed by column name
func (f *Feed) Each(name string, fn func(row map[string]string) error) error {
	var file *zip.File
	for _, zf := range f.z.File {
		//some feeds put files in a sub folder
		if zf.Name == name || strings.HasSuffix(zf.Name, "/"+name) {
			file = zf
			break
		}
	}
	if file == nil {
		return fmt.Errorf("%s not in zip", name)
	}

	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	r := csv.NewReader(rc)
	r.ReuseRecord = true
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return err
	}
	cols := make([]string, len(header))
	for i, h := range header {
		cols[i] = strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))
	}

	row := map[string]string{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		for i, c := range cols {
			if i < len(rec) {
				row[c] = strings.TrimSpace(rec[i])
			} else {
				row[c] = ""
			}
		}
		if err := fn(row); err != nil {
			return err
		}
	}
}

func (f *Feed) Has(name string) bool {
	for _, zf := range f.z.File {
		if zf.Name == name || strings.HasSuffix(zf.Name, "/"+name) {
			return true
		}
	}
	return false
}
