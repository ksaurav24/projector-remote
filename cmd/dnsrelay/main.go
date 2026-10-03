// Minimal UDP DNS forwarder for Android: a static Go binary has no
// bionic resolver and Android has no /etc/resolv.conf, so Go's net
// package falls back to 127.0.0.1:53. Listening there makes DNS work
// for static Go binaries with no filesystem changes.
package main

import (
	"flag"
	"log"
	"net"
	"sync"
	"time"
)

func main() {
	upstream := flag.String("upstream", "192.168.122.1:53", "comma-free single upstream DNS server")
	flag.Parse()

	var wg sync.WaitGroup
	for _, addr := range []string{"127.0.0.1:53", "[::1]:53"} {
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			log.Printf("listen %s: %v (continuing)", addr, err)
			continue
		}
		log.Printf("listening on %s -> %s", addr, *upstream)
		wg.Add(1)
		go func(pc net.PacketConn) {
			defer wg.Done()
			serve(pc, *upstream)
		}(pc)
	}
	wg.Wait()
	log.Fatal("no listeners available")
}

func serve(pc net.PacketConn, upstream string) {
	buf := make([]byte, 4096)
	for {
		n, client, err := pc.ReadFrom(buf)
		if err != nil {
			log.Printf("read: %v", err)
			continue
		}
		q := make([]byte, n)
		copy(q, buf[:n])
		go func() {
			resp, err := ask(upstream, q)
			if err != nil {
				log.Printf("upstream: %v", err)
				return
			}
			pc.WriteTo(resp, client)
		}()
	}
}

func ask(upstream string, q []byte) ([]byte, error) {
	c, err := net.DialTimeout("udp", upstream, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(q); err != nil {
		return nil, err
	}
	resp := make([]byte, 4096)
	n, err := c.Read(resp)
	if err != nil {
		return nil, err
	}
	return resp[:n], nil
}
