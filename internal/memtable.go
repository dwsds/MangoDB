package internal

import (
	sstable "MangoDB/SSTable"
	"math/rand"
	"time"
)

const maxLevel = 6
const p = 0.5
const memtableLimit = 50 // max entries before flush

type Node struct {
	key     string
	value   string
	forward []*Node
}

type SkipList struct {
	header *Node
	level  int
	size   int
}

type Snapshot struct {
	Memtable *SkipList         // Snapshot of memtable at seq
	SSTables [][]sstable.Entry // All sstable entry slices relevant at this seq
	Sequence uint64
	Released bool
}

func (s *Snapshot) Get(key string) (string, bool) {
	// Search in memtable snapshot
	if val, ok := s.Memtable.Get(key); ok {
		return val.(string), true
	}

	// Search in SSTables
	for _, level := range s.SSTables {
		for _, entry := range level {
			if entry.Key == key && entry.SequenceNumber <= s.Sequence {
				return entry.Value, true
			}
		}
	}
	return "", false
}

func newNode(level int, key, value string) *Node {
	return &Node{
		key:     key,
		value:   value,
		forward: make([]*Node, level+1),
	}
}

func NewSkipList() *SkipList {
	rand.Seed(time.Now().UnixNano())
	return &SkipList{
		header: newNode(maxLevel, "", ""),
		level:  0,
		size:   0,
	}
}

func (sl *SkipList) Clone() *SkipList {
	clone := NewSkipList()
	x := sl.header.forward[0]
	for x != nil {
		clone.Insert(x.key, x.value)
		x = x.forward[0]
	}
	return clone
}

func (sl *SkipList) Get(key string) (interface{}, bool) {
	val, ok := sl.Search(key)
	if ok {
		return val, true
	}
	return nil, false
}

func (sl *SkipList) randomLevel() int {
	lvl := 0
	for rand.Float64() < p && lvl < maxLevel {
		lvl++
	}
	return lvl
}

func (sl *SkipList) Insert(key, value string) {
	update := make([]*Node, maxLevel+1)
	x := sl.header

	for i := sl.level; i >= 0; i-- {
		for x.forward[i] != nil && x.forward[i].key < key {
			x = x.forward[i]
		}
		update[i] = x
	}
	x = x.forward[0]

	if x != nil && x.key == key {
		x.value = value
		return
	}

	lvl := sl.randomLevel()
	if lvl > sl.level {
		for i := sl.level + 1; i <= lvl; i++ {
			update[i] = sl.header
		}
		sl.level = lvl
	}

	newNode := newNode(lvl, key, value)
	for i := 0; i <= lvl; i++ {
		newNode.forward[i] = update[i].forward[i]
		update[i].forward[i] = newNode
	}

	sl.size++
}

func (sl *SkipList) Search(key string) (string, bool) {
	x := sl.header
	for i := sl.level; i >= 0; i-- {
		for x.forward[i] != nil && x.forward[i].key < key {
			x = x.forward[i]
		}
	}
	x = x.forward[0]
	if x != nil && x.key == key {
		return x.value, true
	}
	return "", false
}

func (sl *SkipList) Delete(key string) bool {
	update := make([]*Node, maxLevel+1)
	x := sl.header

	for i := sl.level; i >= 0; i-- {
		for x.forward[i] != nil && x.forward[i].key < key {
			x = x.forward[i]
		}
		update[i] = x
	}

	x = x.forward[0]
	if x == nil || x.key != key {
		return false
	}

	for i := 0; i <= sl.level; i++ {
		if update[i].forward[i] != x {
			break
		}
		update[i].forward[i] = x.forward[i]
	}
	sl.size--
	return true
}

func (sl *SkipList) Reset() {
	sl.header = newNode(maxLevel, "", "")
	sl.level = 0
	sl.size = 0
}

func (sl *SkipList) IsFull() bool {
	return sl.size >= memtableLimit
}

func (sl *SkipList) GetAll() map[string]string {
	result := make(map[string]string)
	x := sl.header.forward[0]
	for x != nil {
		result[x.key] = x.value
		x = x.forward[0]
	}
	return result
}
