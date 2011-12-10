package bdb

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// #cgo LDFLAGS: -ldb
// #include <db.h>
// #include <stdlib.h>
// #include <fcntl.h>
//
// #if DB_VERSION_MAJOR+0 >= 3
// typedef struct {void* data; size_t size;} str;
// #define TNXID_ARG NULL,
// static DB* create(const char* path, int flags) {
//	DB* db;
//      int err = db_create(&db, NULL, 0);
//      if (err) return NULL;
//      err = db->open(db, NULL, path, NULL, DB_HASH, flags, 0666);
//	if (err) {
//		db->close(db, 0);
//		return NULL;
//	}
//	return db;
// }
// static int db_close(DB* db) {
//	return db->close(db, 0);
// }
//
// #else
// typedef DBT str;
// #define TNXID_ARG
// #define DB_CREATE (O_CREAT|O_RDWR)
// #define DB_RDONLY O_RDONLY
// #define DB_NOOVERWRITE R_NOOVERWRITE
// static DB* create(const char* path, int flags) {
//	return dbopen(path, flags, 0666, DB_HASH, NULL);
// }
// static int db_close(DB* db) {
//	return db->close(db);
// }
// #endif
//
// static int db_del(DB* db, str* key) {
//	DBT k = {};
//	k.data = key->data;
//	k.size = key->size;
//	return db->del(db, TNXID_ARG &k, 0);
// }
// static int db_get(DB* db, str* key, str* val) {
//	int err;
//	DBT k = {};
//	DBT v = {};
//	k.data = key->data;
//	k.size = key->size;
//	err = db->get(db, TNXID_ARG &k, &v, 0);
//	if (err) return err;
//	val->data = v.data;
//	val->size = v.size;
//	return 0;
// }
// static int db_put(DB* db, str* key, str* val, u_int flags) {
//	DBT k = {};
//	DBT v = {};
//	k.data = key->data;
//	k.size = key->size;
//	v.data = val->data;
//	v.size = val->size;
//	return db->put(db, TNXID_ARG &k, &v, flags);
// }
// static int db_sync(DB* db) {
//	return db->sync(db, 0);
// }
import "C"

type ErrKeyAlreadyExists string
type ErrKeyNotFound string

func (s ErrKeyAlreadyExists) Error() string {
	return fmt.Sprintf("bdb: key '%s' already exists", string(s))
}

func (s ErrKeyNotFound) Error() string {
	return fmt.Sprintf("bdb: key '%s' not found", string(s))
}

type DB struct {
	db *C.DB
	lk sync.Mutex
}

func Create(path string) (*DB, error) {
	return create(path, C.DB_CREATE)
}

func Open(path string) (*DB, error) {
	return create(path, C.DB_RDONLY)
}

func create(path string, flags int) (*DB, error) {
	pstr := C.CString(path)
	defer C.free(unsafe.Pointer(pstr))

	db, err := C.create(pstr, C.int(flags))

	if db == nil {
		return nil, err
	}

	ret := &DB{db, sync.Mutex{}}
	runtime.SetFinalizer(ret, destroy)
	return ret, nil
}

func destroy(db *DB) {
	db.Close()
}

func (db *DB) Close() error {
	db.lk.Lock()
	defer db.lk.Unlock()

	if db.db == nil {
		return nil
	}

	n, err := C.db_close(db.db)
	db.db = nil

	if n != 0 {
		return err
	}

	return nil
}

func (db *DB) set(key, value []byte, flags int) error {
	db.lk.Lock()
	defer db.lk.Unlock()

	k := C.str{unsafe.Pointer(&key[0]), C.size_t(len(key))}
	v := C.str{unsafe.Pointer(&value[0]), C.size_t(len(value))}

	n, err := C.db_put(db.db, &k, &v, C.u_int(flags))

	switch {
	case n < 0:
		return err
	case n > 0:
		return ErrKeyAlreadyExists(string(key))
	}

	return nil
}

func (db *DB) Set(key, value []byte) error {
	return db.set(key, value, 0)
}

func (db *DB) Add(key, value []byte) error {
	return db.set(key, value, C.DB_NOOVERWRITE)
}

func (db *DB) Get(key []byte) ([]byte, error) {
	db.lk.Lock()
	defer db.lk.Unlock()

	k := C.str{unsafe.Pointer(&key[0]), C.size_t(len(key))}
	v := C.str{}

	n, err := C.db_get(db.db, &k, &v)

	switch {
	case n < 0:
		return []byte{}, err
	case n > 0:
		return []byte{}, ErrKeyNotFound(string(key))
	}

	return C.GoBytes(v.data, C.int(v.size)), nil
}

func (db *DB) Flush() error {
	db.lk.Lock()
	defer db.lk.Unlock()

	n, err := C.db_sync(db.db)

	if n < 0 {
		return err
	}

	return nil
}

func (db *DB) Remove(key []byte) error {
	db.lk.Lock()
	defer db.lk.Unlock()

	k := C.str{unsafe.Pointer(&key[0]), C.size_t(len(key))}

	n, err := C.db_del(db.db, &k)

	switch {
	case n < 0:
		return err
	case n > 0:
		return ErrKeyNotFound(string(key))
	}

	return nil
}
