# python version of main.go, same logic
# pip install gtfs-realtime-bindings ; python3 feed_check.py gtfs.zip out.json
import csv, io, sys, time, zipfile, json, datetime, urllib.request
from google.transit import gtfs_realtime_pb2

URL = "https://bustime.ttc.ca/gtfsrt/vehicles"
N, EVERY = 50, 30
zpath, out = sys.argv[1], sys.argv[2]

z = zipfile.ZipFile(zpath)
static = set()
with z.open("trips.txt") as f:
    for r in csv.DictReader(io.TextIOWrapper(f, "utf-8-sig")):
        static.add(r["trip_id"].strip())

def get(u):
    with urllib.request.urlopen(u, timeout=20) as r:
        return r.status, r.read()

total = with_trip = matched = ok = 0
unmatched = {}
per_snap = []
for i in range(N):
    if i:
        time.sleep(EVERY)
    try:
        st, body = get(URL)
        m = gtfs_realtime_pb2.FeedMessage(); m.ParseFromString(body)
    except Exception as e:
        print("snap", i + 1, "failed", e, flush=True); continue
    ok += 1
    n = nm = 0
    for e in m.entity:
        if not e.HasField("vehicle") or not e.vehicle.HasField("position"):
            continue
        total += 1; n += 1
        tid = e.vehicle.trip.trip_id
        if not tid:
            continue
        with_trip += 1
        if tid in static:
            matched += 1; nm += 1
        elif len(unmatched) < 20:
            unmatched[tid] = unmatched.get(tid, 0) + 1
    per_snap.append((n, nm))
    print("snap", i + 1, n, nm, flush=True)

res = dict(run_at=datetime.datetime.now().astimezone().isoformat(), snapshots_ok=ok, n=N,
           pings=total, with_trip=with_trip, matched=matched, static_trips=len(static),
           rate=100 * matched / total if total else None, unmatched=list(unmatched),
           per_snap=per_snap, other={})
for p in ["/trips", "/alerts"]:
    try:
        st, body = get(URL.replace("/vehicles", p))
        m = gtfs_realtime_pb2.FeedMessage(); m.ParseFromString(body)
        res["other"][p] = f"status {st}, decodes, {len(m.entity)} entities"
    except Exception as e:
        res["other"][p] = str(e)
json.dump(res, open(out, "w"), indent=1)
print("DONE", json.dumps({k: res[k] for k in ["snapshots_ok", "pings", "with_trip", "matched", "rate"]}))
