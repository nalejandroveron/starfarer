package netcode

import "time"

// Stop-and-wait is intentional: ordered actions have an independent bounded
// queue, so retransmission never blocks movement or snapshots. Ack means that
// the application accepted the payload. Durable side effects need a DB ledger.
type ReliablePacket struct {
	Sequence uint64
	Payload  []byte
}
type ReliableChannel struct {
	Sent, Received uint64
	queue          []ReliablePacket
	next           time.Time
}

func (r *ReliableChannel) Enqueue(data []byte) bool {
	if len(data) == 0 || len(data) > 512 || len(r.queue) >= 64 || r.Sent == ^uint64(0) {
		return false
	}
	r.Sent++
	r.queue = append(r.queue, ReliablePacket{r.Sent, append([]byte(nil), data...)})
	return true
}
func (r *ReliableChannel) Due(now time.Time, retry time.Duration) (ReliablePacket, bool) {
	if len(r.queue) == 0 || now.Before(r.next) {
		return ReliablePacket{}, false
	}
	if retry < 100*time.Millisecond {
		retry = 100 * time.Millisecond
	}
	if retry > time.Second {
		retry = time.Second
	}
	r.next = now.Add(retry)
	return r.queue[0], true
}
func (r *ReliableChannel) Ack(sequence uint64) bool {
	if len(r.queue) == 0 || r.queue[0].Sequence != sequence {
		return false
	}
	r.queue = r.queue[1:]
	r.next = time.Time{}
	return true
}
func (r *ReliableChannel) Receive(sequence uint64, data []byte, accept func([]byte) bool) uint64 {
	if sequence == r.Received+1 && sequence != 0 && len(data) > 0 && len(data) <= 512 && accept(data) {
		r.Received = sequence
	}
	return r.Received
}
func (r *ReliableChannel) Pending() int { return len(r.queue) }
