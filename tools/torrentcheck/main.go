// Command torrentcheck is a quick live check of the built-in torrent client:
// it joins a public-domain torrent (Sintel, Blender Foundation, CC BY) and
// reports peers, incoming connections, UPnP/port-mapping status and download
// speed for a short while, then stops and deletes what it fetched.
//
//	go run ./tools/torrentcheck [-seconds 45]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ryanborg/mediarium/internal/torrentclient"
)

const sintel = "magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10&dn=Sintel&tr=udp%3A%2F%2Fexplodie.org%3A6969&tr=udp%3A%2F%2Ftracker.coppersurfer.tk%3A6969&tr=udp%3A%2F%2Ftracker.empire-js.us%3A1337&tr=udp%3A%2F%2Ftracker.leechers-paradise.org%3A6969&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337&tr=wss%3A%2F%2Ftracker.btorrent.xyz&tr=wss%3A%2F%2Ftracker.fastcast.nz&tr=wss%3A%2F%2Ftracker.openwebtorrent.com&ws=https%3A%2F%2Fwebtorrent.io%2Ftorrents%2F"

func main() {
	seconds := flag.Int("seconds", 45, "how long to download for")
	flag.Parse()

	dir, err := os.MkdirTemp("", "torrentcheck")
	if err != nil {
		fmt.Println("temp dir:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	c, err := torrentclient.New(torrentclient.Config{DataDir: dir})
	if err != nil {
		fmt.Println("start client:", err)
		os.Exit(1)
	}
	defer c.Close()
	fmt.Println("listening on:", c.ListenAddrs())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t, err := c.AddMagnet(ctx, sintel)
	cancel()
	if err != nil {
		fmt.Println("metadata:", err)
		os.Exit(1)
	}
	fmt.Printf("got metadata: %q, %.1f MB\n", t.Name(), float64(t.Length())/1e6)
	t.DownloadAll()

	start := time.Now()
	var last int64
	for time.Since(start) < time.Duration(*seconds)*time.Second {
		time.Sleep(5 * time.Second)
		st := t.Stats()
		done := t.BytesCompleted()
		fmt.Printf("%3.0fs  peers=%d active=%d seeders=%d  done=%.1f MB  speed=%.2f MB/s\n",
			time.Since(start).Seconds(), st.TotalPeers, st.ActivePeers, st.ConnectedSeeders,
			float64(done)/1e6, float64(done-last)/5/1e6)
		last = done
		if done >= t.Length() {
			fmt.Println("complete")
			break
		}
	}
	fmt.Println("done; the downloaded data is deleted now")
}
