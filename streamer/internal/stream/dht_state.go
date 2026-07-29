package stream

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"time"

	dht "github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
)

// dhtSavedAddr is the on-disk format for a single DHT routing-table entry.
type dhtSavedAddr struct {
	IP   net.IP `json:"ip"`
	Port int    `json:"port"`
}

// dhtStartingNodes returns a DhtStartingNodes func that seeds the routing table
// with both the global bootstrap nodes and any nodes saved in stateFile. This
// makes DHT immediately useful after a restart instead of waiting 30-90 seconds
// for the routing table to repopulate from scratch.
func dhtStartingNodes(stateFile string) func(string) dht.StartingNodesGetter {
	return func(network string) dht.StartingNodesGetter {
		return func() ([]dht.Addr, error) {
			nodes, _ := dht.GlobalBootstrapAddrs(network)
			data, err := os.ReadFile(stateFile)
			if err != nil {
				return nodes, nil // state file doesn't exist yet
			}
			var saved []dhtSavedAddr
			if err := json.Unmarshal(data, &saved); err != nil {
				log.Printf("streamer: dht state load: %v", err)
				return nodes, nil
			}
			for _, a := range saved {
				nodes = append(nodes, dht.NewAddr(&net.UDPAddr{IP: a.IP, Port: a.Port}))
			}
			log.Printf("streamer: dht state loaded %d saved nodes from %s", len(saved), stateFile)
			return nodes, nil
		}
	}
}

// saveDHTState writes the current routing table to stateFile.
func saveDHTState(c *torrent.Client, stateFile string) {
	var addrs []dhtSavedAddr
	for _, s := range c.DhtServers() {
		if w, ok := s.(torrent.AnacrolixDhtServerWrapper); ok {
			for _, ni := range w.Server.Nodes() {
				addrs = append(addrs, dhtSavedAddr{IP: ni.Addr.IP, Port: ni.Addr.Port})
			}
		}
	}
	if len(addrs) == 0 {
		return
	}
	data, err := json.Marshal(addrs)
	if err != nil {
		log.Printf("streamer: dht state marshal: %v", err)
		return
	}
	if err := os.WriteFile(stateFile, data, 0o644); err != nil {
		log.Printf("streamer: dht state save: %v", err)
		return
	}
	log.Printf("streamer: dht state saved %d nodes to %s", len(addrs), stateFile)
}

// startDHTStateSaver periodically saves the DHT routing table. Returns a stop
// function that flushes a final save and waits for the goroutine to exit.
func startDHTStateSaver(c *torrent.Client, stateFile string, interval time.Duration) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				saveDHTState(c, stateFile)
				return
			case <-ticker.C:
				saveDHTState(c, stateFile)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
