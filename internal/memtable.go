package internal

import (
	sstable "mangodb/SSTable"
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxLevel = 6
const p = 0.5
const memtableLimit = 50 

type Node struct {
	key     string
	value   string
	forward []*Node
}

type Entry struct {
	Key   string
	Value string
}

type SkipList struct {
	header *Node
	level  int
	size   int
}

type Snapshot struct {
	Memtable *SkipList         
	SSTables [][]sstable.Entry 
	Sequence uint64
	Released bool
}
type SkipListIterator struct {
	current *Node
}

func (s *SkipList) Iterator() *SkipListIterator {
	
	return &SkipListIterator{current: s.header.forward[0]}
}

func (it *SkipListIterator) HasNext() bool {
	return it.current != nil
}

func (it *SkipListIterator) Next() *Entry {
	entry := &Entry{
		Key:   it.current.key,
		Value: it.current.value,
	}
	it.current = it.current.forward[0]
	return entry
}

func (s *Snapshot) Get(key string) (string, bool) {

	if val, ok := s.Memtable.Get(key); ok {
		return val.(string), true
	}

	for _, level := range s.SSTables {
		for _, entry := range level {
			if entry.Key == key && entry.SequenceNumber <= s.Sequence {
				return entry.Value, true
			}
		}
	}
	return "", false
}

func (s *Snapshot) SaveToFile(filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	fmt.Fprintf(writer, "SEQ|%d\n", s.Sequence)

	iter := s.Memtable.Iterator()
	for iter.HasNext() {
		entry := iter.Next()
		fmt.Fprintf(writer, "PUT|%s|%s\n", entry.Key, entry.Value)
	}
	for _, table := range s.SSTables {
		for _, entry := range table {
			fmt.Fprintf(writer, "SST|%s|%s\n", entry.Key, entry.Value)
		}
	}

	return writer.Flush()
}

func RestoreSnapshot(filePath string) (*Snapshot, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	memtable := NewSkipList()
	var sstables [][]sstable.Entry
	var currentSST []sstable.Entry

	var inSST bool
	var seq uint64

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "|")
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "SEQ":
			seq, _ = strconv.ParseUint(parts[1], 10, 64)

		case "PUT":
			if len(parts) < 3 {
				continue
			}
			memtable.Insert(parts[1], parts[2])

		case "SST_BEGIN":
			currentSST = nil
			inSST = true

		case "SST":
			if !inSST || len(parts) < 3 {
				continue
			}
			currentSST = append(currentSST, sstable.Entry{Key: parts[1], Value: parts[2]})

		case "SST_END":
			if inSST {
				sstables = append(sstables, currentSST)
				inSST = false
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &Snapshot{
		Memtable: memtable,
		SSTables: sstables,
		Sequence: seq,
	}, nil
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

