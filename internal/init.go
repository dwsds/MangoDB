package internal

import (
	sstable "mangodb/SSTable"
	"fmt"
)

type DB struct {
	memtable *SkipList
	wal      *WAL
	seq      uint64
	sstables [][]sstable.Entry
}

func (db *DB) GetMemtable() *SkipList {
	return db.memtable
}

func NewDB(walPath string) (*DB, error) {
	wal, err := NewWAL(walPath)
	if err != nil {
		return nil, err
	}
	memtable := NewSkipList()

	db := &DB{
		memtable: memtable,
		wal:      wal,
		seq:      0, 
	}


	err = wal.Load(memtable)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) Put(key, value string) error {
	db.seq++ 

	err := db.wal.Append("PUT", key, value, db.seq)
	if err != nil {
		return err
	}
	db.memtable.Insert(key, value)

	if db.memtable.IsFull() {
		err := db.Flush()
		if err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) Get(key string) (string, bool) {
	return db.memtable.Search(key)
}

func (db *DB) Delete(key string) error {
	db.seq++

	err := db.wal.Append("DEL", key, "", db.seq) 
	if err != nil {
		return err
	}
	db.memtable.Delete(key)
	return nil
}

func (db *DB) Flush() error {
	fmt.Println("Flushing memtable to SSTable...")
	dataMap := db.memtable.GetAll()
	entries := make([]sstable.Entry, 0, len(dataMap))
	for k, v := range dataMap {
		entries = append(entries, sstable.Entry{
			Key:   k,
			Value: v,
		})
	}

	filenames, newSeq, err := sstable.WriteSSTables("sstable", entries, 50, 50, db.seq)
	if err != nil {
		return err
	}
	fmt.Println("Flushed SSTables:", filenames)
	db.seq = newSeq

	db.memtable.Reset()
	return db.wal.Reset()
}

func (db *DB) CreateSnapshot() *Snapshot {


	snapshot := &Snapshot{
		Sequence: db.seq,
		Memtable: db.memtable.Clone(), 
		SSTables: db.sstables,        
	}
	return snapshot
}



func (db *DB) Restore(snapshot *Snapshot) {
	db.memtable = snapshot.Memtable
	db.seq = snapshot.Sequence
	db.sstables = snapshot.SSTables
}
