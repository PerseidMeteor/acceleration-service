package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	bolt "go.etcd.io/bbolt"
)

var bucketObjectNodes = []byte("nodes")

const NodeMaximumKeepPeriod = time.Hour * 24

const NodeStatusProcessing = "PROCESSING"
const NodeStatusCompleted = "COMPLETED"
const NodeStatusFailed = "FAILED"

// const metaMaximumKeepPeriod = time.Hour * 24

const metaStatusProcessing = "PROCESSING"
const metaStatusCompleted = "COMPLETED"
const metaStatusFailed = "FAILED"

// meta for uncompress image info
type Meta struct {
	ID             string    `json:"id"`              //ID
	Image          string    `json:"image"`           //镜像
	IsProfiling    bool      `json:"is_profiling"`    //是否扩充过
	Path           string    `json:"path"`            //工作路径，用于将对文件的获取，重定向至此地址
	UncompressDirs []string  `json:"uncompress_dirs"` //数据层地址，用于后续向数据层更新、添加文件
	Status         string    `json:"status"`          //状态
	Created        time.Time `json:"created"`         //创建时间
	Finished       time.Time `json:"finished"`        //结束时间
}

// Node for node info
type Node struct {
	ID      string           `json:"id"`      // ID
	Name    string           `json:"name"`    // node name
	Metas   map[string]*Meta `json:"metas"`   // image meta data on node, key is image reference, value is image meta dir
	Path    string           `json:"path"`    // share layer path
	Status  string           `json:"status"`  // node status
	Created time.Time        `json:"created"` // create time
	Updated time.Time        `json:"updated"` // update time
}

type nodeManager struct {
	mutex   sync.Mutex
	db      *bolt.DB
	nodes   map[string]*Node
	nodeDir string
}

var NodeManager *nodeManager

func init() {
	NodeManager = &nodeManager{
		mutex: sync.Mutex{},
		nodes: make(map[string]*Node),
	}
}

func (n *Node) IsExpired() bool {
	if n.Status != NodeStatusProcessing &&
		time.Now().After(n.Updated.Add(NodeMaximumKeepPeriod)) {
		return true
	}
	return false
}

// Init manager supported by boltdb.
func (m *nodeManager) Init(workDir string, nodeDir string) error {
	// create node dir
	if err := os.MkdirAll(nodeDir, 0755); err != nil {
		return errors.Wrap(err, "create node dir")
	}

	bdb, err := bolt.Open(filepath.Join(workDir, "nodes.db"), 0655, nil)
	if err != nil {
		return errors.Wrap(err, "create node database")
	}
	m.db = bdb
	m.nodeDir = nodeDir
	return m.initDatabase()
}

// initDatabase loads nodes from the database into memory.
func (m *nodeManager) initDatabase() error {
	return m.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("nodes"))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, v []byte) error {
			var node Node
			if err := json.Unmarshal(v, &node); err != nil {
				return err
			}
			// if node.Status == NodeStatusProcessing {
			// 	return bucket.Delete([]byte(node.ID))
			// }
			m.nodes[node.ID] = &node
			return nil
		})
	})
}

// updateBucket updates node in bucket and creates a new bucket if it doesn't already exist.
func (m *nodeManager) updateBucket(node *Node) error {
	return m.db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(bucketObjectNodes)
		if err != nil {
			return err
		}

		nodeJSON, err := json.Marshal(node)
		if err != nil {
			return err
		}

		return bucket.Put([]byte(node.ID), nodeJSON)
	})
}

// deleteBucket deletes a node in bucket
func (m *nodeManager) deleteBucket(nodeID string) error {
	return m.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(bucketObjectNodes)
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(nodeID))
	})
}

// Create node data
func (m *nodeManager) Create(name string) (string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	id := uuid.NewString()
	path := filepath.Join(m.nodeDir, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		return "", errors.Wrap(err, "create node dir")
	}

	node := &Node{
		ID:      id,
		Name:    name,
		Metas:   map[string]*Meta{},
		Path:    path,
		Created: time.Now(),
		Updated: time.Now(),
		Status:  NodeStatusProcessing,
	}
	m.nodes[id] = node
	if err := m.updateBucket(node); err != nil {
		return "", err
	}
	m.nodes[id] = node
	return id, nil
}

func (m *nodeManager) Finish(id string, err error) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Update node status.
	node := m.nodes[id]
	if node != nil {
		if err != nil {
			node.Status = NodeStatusFailed
		} else {
			node.Status = NodeStatusCompleted
		}
		node.Updated = time.Now()
	}
	if err := m.updateBucket(node); err != nil {
		return err
	}

	// Evict expired nodes.
	for id, node := range m.nodes {
		if node.IsExpired() {
			delete(m.nodes, id)
			if err := m.deleteBucket(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *nodeManager) List() []*Node {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	nodes := make([]*Node, 0)
	for _, node := range m.nodes {
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Created.After(nodes[j].Created)
	})

	return nodes
}

// Find node with node name and return node id and node path string
// TODO: fix later
func (m *nodeManager) Find(node string) (string, string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, n := range m.nodes {
		if n.Name == node {
			return n.ID, n.Path, nil
		}
	}

	return "", "", fmt.Errorf("obtain image %s", node)
}

// Append image record to node
func (m *nodeManager) AppendImagetoNode(nodeID, image, metaDir string, dataPath []string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	node := m.nodes[nodeID]
	id := uuid.NewString()

	meta := Meta{
		ID:             id,
		Image:          image,
		Path:           metaDir,
		UncompressDirs: dataPath,
		Created:        time.Now(),
		Status:         metaStatusProcessing,
	}

	node.Metas[image] = &meta

	// write node to bucket
	return m.updateBucket(node)
}

func (m *nodeManager) ObtainMetaAndUcp(nodeName, ref, file string) (string, []string, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var nodeID string
	for _, n := range m.nodes {
		if n.Name == nodeName {
			nodeID = n.ID
		}
	}
	node := m.nodes[nodeID]
	if node == nil {
		return "", nil, fmt.Errorf("no such node %s", nodeName)
	}

	meta := node.Metas[ref]
	if meta == nil {
		return "", nil, fmt.Errorf("no such refernece %s meta ", ref)
	}
	return meta.Path, meta.UncompressDirs, nil
}
