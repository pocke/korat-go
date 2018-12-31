package main

import "sync"

type UnreadCountNotifier struct {
	chs []chan *UnreadCount
	mu  sync.Mutex
}

var unreadCountNotifier = NewUnreadCountNotifier()

func NewUnreadCountNotifier() *UnreadCountNotifier {
	return &UnreadCountNotifier{}
}

type UnreadCount struct {
	ChannelID int
	Count     int
}

func (n *UnreadCountNotifier) Subscribe() <-chan *UnreadCount {
	ch := make(chan *UnreadCount)
	n.mu.Lock()
	defer n.mu.Unlock()
	n.chs = append(n.chs, ch)

	return ch
}

func (n *UnreadCountNotifier) Unsubscribe(ch <-chan *UnreadCount) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for idx, c := range n.chs {
		if ch == c {
			chs := append(n.chs[:idx], n.chs[idx+1:]...)
			newChs := make([]chan *UnreadCount, len(chs))
			copy(newChs, chs)
			n.chs = newChs
			return
		}
	}
}

func (n *UnreadCountNotifier) Notify(msg *UnreadCount) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for _, c := range n.chs {
		c <- msg
	}
}
