package api

import "testing"

func TestEventHubCaps(t *testing.T) {
	prev := eventLimits
	eventLimits.perUser, eventLimits.perIP = 2, 3
	defer func() { eventLimits = prev }()

	for _, c := range []struct {
		name string
		join [][2]string // user, ip
		want []bool
	}{
		{"per user", [][2]string{{"1", "a"}, {"1", "b"}, {"1", "c"}}, []bool{true, true, false}},
		{"users do not share", [][2]string{{"1", "a"}, {"2", "b"}, {"1", "c"}, {"2", "d"}}, []bool{true, true, true, true}},
		{"per address", [][2]string{{"1", "a"}, {"2", "a"}, {"3", "a"}, {"4", "a"}}, []bool{true, true, true, false}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var h eventHub
			for i, j := range c.join {
				_, ok := h.join(j[0], j[1])
				if ok != c.want[i] {
					t.Errorf("join %d %v = %v, want %v", i, j, ok, c.want[i])
				}
			}
		})
	}
}

func TestEventHubLeaveFreesTheSlot(t *testing.T) {
	var h eventHub
	subs := make([]*eventSub, 0, eventLimits.perUser)
	for i := 0; i < eventLimits.perUser; i++ {
		s, ok := h.join("1", "a")
		if !ok {
			t.Fatalf("join %d refused", i)
		}
		subs = append(subs, s)
	}
	if _, ok := h.join("1", "a"); ok {
		t.Fatal("one more than the cap was let in")
	}
	h.leave(subs[0])
	h.leave(subs[0]) // twice is harmless
	if h.open() != eventLimits.perUser-1 {
		t.Errorf("open = %d", h.open())
	}
	if _, ok := h.join("1", "a"); !ok {
		t.Error("the slot was not given back")
	}
}

func TestEventHubPublishNeverBlocks(t *testing.T) {
	var h eventHub
	slow, _ := h.join("1", "a")
	for i := 0; i < eventsBuffer+5; i++ {
		h.publish("activity", "{}") // nobody reads: must not block
	}
	if !h.takeDropped(slow) {
		t.Error("a full stream was not marked")
	}
	if h.takeDropped(slow) {
		t.Error("the mark was not cleared")
	}
	if len(slow.ch) != eventsBuffer {
		t.Errorf("buffered %d", len(slow.ch))
	}
}

func TestEventHubShutdownEndsStreams(t *testing.T) {
	var h eventHub
	sub, _ := h.join("1", "a")
	h.shutdown()
	if _, open := <-sub.ch; open {
		t.Error("the stream was not closed")
	}
	if _, ok := h.join("1", "a"); ok {
		t.Error("a stream joined after shutdown")
	}
	h.leave(sub) // harmless after shutdown
}
