package httpdebug

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

// detailedTiming collects durations only, never hostnames, IPs or error prose.
// Callbacks may run concurrently and may finish after RoundTrip returns.
type detailedTiming struct {
	mu                           sync.Mutex
	start, dnsStart, tlsStart    time.Time
	connectStarts                map[[2]string][]time.Time
	dns, connect, tls, firstByte time.Duration
	reused                       bool
}

func (d *detailedTiming) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSStart: func(httptrace.DNSStartInfo) {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.dnsStart = time.Now()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if !d.dnsStart.IsZero() {
				d.dns += time.Since(d.dnsStart)
			}
		},
		ConnectStart: func(network, address string) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if d.connectStarts == nil {
				d.connectStarts = make(map[[2]string][]time.Time)
			}
			key := [2]string{network, address}
			d.connectStarts[key] = append(d.connectStarts[key], time.Now())
		},
		ConnectDone: func(network, address string, _ error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			key := [2]string{network, address}
			starts := d.connectStarts[key]
			if len(starts) != 0 {
				// Sum completed attempts (successful or failed), including
				// overlap; this can exceed wall time. Identical keys pair FIFO.
				d.connect += time.Since(starts[0])
				if len(starts) == 1 {
					delete(d.connectStarts, key)
				} else {
					d.connectStarts[key] = starts[1:]
				}
			}
		},
		TLSHandshakeStart: func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.tlsStart = time.Now()
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			if !d.tlsStart.IsZero() {
				d.tls += time.Since(d.tlsStart)
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.reused = info.Reused
		},
		GotFirstResponseByte: func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			// An informational response (notably 100 Continue) can arrive
			// before WroteRequest. Measure from the base transport start,
			// not the end of the upload. Remains zero if no callback is observed.
			if !d.start.IsZero() {
				d.firstByte = time.Since(d.start)
			}
		},
	}
}

func (d *detailedTiming) log(t *tracer, id uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	t.log(id, "timing", "dns=%s connect=%s tls=%s first_byte=%s connection_reused=%t", d.dns, d.connect, d.tls, d.firstByte, d.reused)
}
