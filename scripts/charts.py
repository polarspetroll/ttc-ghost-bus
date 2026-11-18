#make the 2 readme charts from docs/data/*.csv (run scripts/export.sh first)
import csv
import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt


def read(path):
    with open(path) as f:
        return list(csv.DictReader(f))


routes = read("docs/data/ghosts_by_route.csv")[:15]
if not routes:
    raise SystemExit("no data yet, run the poller + aggregate first")

names = [r["name"] for r in routes][::-1]
ghosts = [int(r["ghosts"]) for r in routes][::-1]

fig, ax = plt.subplots(figsize=(8, 5))
ax.barh(names, ghosts, color="#d9534f")
ax.set_xlabel("ghost trips (weekdays)")
ax.set_title("Top 15 routes by ghost trips")
ax.spines[["top", "right"]].set_visible(False)
fig.tight_layout()
fig.savefig("docs/images/ghosts_by_route.png", dpi=150)


weeks = read("docs/data/weekly.csv")
x = [w["week_of"] for w in weeks]
rate = [100 * int(w["ghosts"]) / int(w["trips"]) for w in weeks]

fig, ax = plt.subplots(figsize=(8, 4))
ax.plot(x, rate, marker="o", color="#337ab7")
ax.set_ylabel("weekday ghost rate (%)")
ax.set_title("Weekday ghost rate by week")
ax.set_ylim(bottom=0)
ax.spines[["top", "right"]].set_visible(False)
plt.xticks(rotation=30)
fig.tight_layout()
fig.savefig("docs/images/weekly_ghost_rate.png", dpi=150)

print("saved docs/images/ghosts_by_route.png and weekly_ghost_rate.png")
