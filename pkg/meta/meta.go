package meta

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"

	// "github.com/sirupsen/logrus"
	bolt "go.etcd.io/bbolt"
)

var bucketObjectFiles = []byte("files")

const StatusProcessing = "PROCESSING"

const metaMaximumKeepPeriod = time.Hour * 24

const metaStatusProcessing = "PROCESSING"
const metaStatusCompleted = "COMPLETED"
const metaStatusFailed = "FAILED"

// meta for uncompress image info
type Meta struct {
	ID          string    `json:"id"`           //ID
	Image       string    `json:"image"`        //镜像
	IsProfiling bool      `json:"is_profiling"` //是否扩充过
	Name        string    `json:"name"`         //名称
	Path        string    `json:"path"`         //工作路径，用于将对文件的获取，重定向至此地址
	DataPath    string    `json:"data_path"`    //数据层地址，用于后续向数据层更新、添加文件
	Status      string    `json:"status"`       //状态
	Created     time.Time `json:"created"`      //创建时间
	Finished    time.Time `json:"finished"`     //结束时间
}

type fileManager struct {
	mutex sync.Mutex
	db    *bolt.DB
	metas map[string]*Meta
}

var FileManager *fileManager

func init() {
	FileManager = &fileManager{
		mutex: sync.Mutex{},
		metas: make(map[string]*Meta),
	}
}

func (m *Meta) IsExpired() bool {
	if m.Status != StatusProcessing &&
		time.Now().After(m.Finished.Add(metaMaximumKeepPeriod)) {
		return true
	}
	return false
}

// Init manager supported by boltdb.
func (m *fileManager) Init(workDir string) error {
	bdb, err := bolt.Open(filepath.Join(workDir, "files.db"), 0655, nil)
	if err != nil {
		return errors.Wrap(err, "create meta database")
	}
	m.db = bdb
	return m.initDatabase()
}

// initDatabase loads metas from the database into memory.
func (m *fileManager) initDatabase() error {
	return m.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("files"))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, v []byte) error {
			var meta Meta
			if err := json.Unmarshal(v, &meta); err != nil {
				return err
			}
			// if meta.Status == StatusProcessing {
			// 	return bucket.Delete([]byte(meta.ID))
			// }
			m.metas[meta.ID] = &meta
			return nil
		})
	})
}

// updateBucket updates meta in bucket and creates a new bucket if it doesn't already exist.
func (m *fileManager) updateBucket(meta *Meta) error {
	return m.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(bucketObjectFiles)
		if err != nil {
			return err
		}

		metaJSON, err := json.Marshal(meta)
		if err != nil {
			return err
		}

		return bucket.Put([]byte(meta.ID), metaJSON)
	})
}

// deleteBucket deletes a meta in bucket
func (m *fileManager) deleteBucket(metaID string) error {
	return m.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketObjectFiles)
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(metaID))
	})
}

// Create meta data for image
func (m *fileManager) Create(source, path string) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	id := uuid.NewString()

	meta := &Meta{
		ID:      id,
		Image:   source,
		Name:    source,
		Path:    path,
		Created: time.Now(),
		Status:  metaStatusProcessing,
	}
	m.metas[id] = meta
	if err := m.updateBucket(meta); err != nil {
		return "", err
	}
	m.metas[id] = meta
	return id, nil
}

func (m *fileManager) Finish(id string, err error) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Update meta status.
	meta := m.metas[id]
	if meta != nil {
		if err != nil {
			meta.Status = metaStatusFailed
			// meta.Reason = err.Error()
		} else {
			meta.Status = metaStatusCompleted
		}
		meta.Finished = time.Now()
	}
	if err := m.updateBucket(meta); err != nil {
		return err
	}

	// Evict expired metas.
	for id, meta := range m.metas {
		if meta.IsExpired() {
			delete(m.metas, id)
			if err := m.deleteBucket(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *fileManager) List() []*Meta {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	metas := make([]*Meta, 0)
	for _, meta := range m.metas {
		metas = append(metas, meta)
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].Created.After(metas[j].Created)
	})

	return metas
}

// Find file uncompress
func (m *fileManager) Find(ref, filePath string) (*Meta, string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, meta := range m.metas {
		if meta.Image == ref {
			return meta, meta.Path, nil
		}
	}

	return nil, "", fmt.Errorf("obtain image %s file %s", ref, filePath)
}
