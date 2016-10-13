package stackpointio

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
)

// ClusterClient is a StackPointCloud API client for a particular cluster
type ClusterClient struct {
	organization int
	id           int
	apiClient    *APIClient
}

// CreateClusterClient creates a cluster client
func CreateClusterClient(organizationID, clusterID int, client *APIClient) *ClusterClient {
	clusterClient := &ClusterClient{
		organization: organizationID,
		id:           clusterID,
		apiClient:    client,
	}
	return clusterClient
}

// NodeManager has a set of nodes and can add or delete them via the StackPointCloud API
type NodeManager struct {
	NodeType      string
	clusterClient *ClusterClient
	nodes         map[string]Node
}

// CreateNodeManager creates a NodeManager
func CreateNodeManager(nodeType string, cluster *ClusterClient) *NodeManager {
	manager := &NodeManager{
		NodeType:      nodeType,
		clusterClient: cluster,
		nodes:         make(map[string]Node),
	}
	return manager
}

// Size returns the number of nodes which are in a "running" state
func (manager *NodeManager) Size() int {
	var running int
	for _, node := range manager.nodes {
		if node.State == "running" {
			running++
		}
	}
	return running
}

func (manager *NodeManager) countStates() *map[string]int {
	counts := map[string]int{"draft": 0, "building": 0, "provisioned": 0, "running": 0, "deleting": 0, "deleted": 0}
	for _, node := range manager.nodes {
		counts[node.State]++
	}
	return &counts
}

func (manager *NodeManager) addNode(node Node) int {
	manager.nodes[node.InstanceID] = node
	return len(manager.nodes)
}

// GetNode returns a Node identified by the stackpointio instanceID
func (manager *NodeManager) GetNode(instanceID string) (Node, bool) {
	node, ok := manager.nodes[instanceID]
	return node, ok
}

// GetNode returns a Node identified by the stackpointio primaryKey
func (manager *NodeManager) GetNodePK(nodePK int) (Node, bool) {
	for _, node := range manager.nodes {
		if node.PrimaryKey == nodePK {
			return node, true
		}
	}
	return Node{}, false
}

// Update refreshes the state of the current nodes in the clusterClient
func (manager *NodeManager) Update() error {
	log.WithFields(log.Fields{
		"organizationID": manager.clusterClient.organization,
		"clusterID":      manager.clusterClient.id,
	}).Info("Updating cluster info")
	clusterNodes, err := manager.clusterClient.apiClient.GetNodes(manager.clusterClient.organization, manager.clusterClient.id)
	if err != nil {
		return err
	}
	for _, clusterNode := range clusterNodes {
		localNode, ok := manager.nodes[clusterNode.InstanceID]
		if ok {
			if localNode.State != clusterNode.State {
				log.WithFields(log.Fields{
					"nodeID":   clusterNode.InstanceID,
					"oldState": localNode.State,
					"newState": clusterNode.State,
				}).Info("Node state change")
			}
		} else {
			log.WithFields(log.Fields{
				"nodeID":   clusterNode.InstanceID,
				"newState": clusterNode.State,
			}).Info("New node found")
		}
		manager.nodes[clusterNode.InstanceID] = clusterNode
	}
	if len(clusterNodes) < len(manager.nodes) {
		log.Info("Remote node count is too small")
		reconciliationSet := make(map[string]int)
		for index, clusterNode := range clusterNodes {
			reconciliationSet[clusterNode.InstanceID] = index
		}
		for instanceID := range manager.nodes {
			_, ok := reconciliationSet[instanceID]
			if !ok {
				log.WithFields(log.Fields{
					"nodeID": instanceID,
				}).Info("Local node does not exist in cluster, removing")
				delete(manager.nodes, instanceID)
			}
		}

	}
	return nil
}

// IncreaseSize adds nodes to the manager and to the cluster, waits until
// the addition is complete.  Returns the count of running nodes.
func (manager *NodeManager) IncreaseSize(additional int) (int, error) {
	manager.Update()
	states := manager.countStates()

	newNodes := NodeAdd{
		Size:  manager.NodeType,
		Count: additional,
	}
	target := (*states)["running"] + additional

	result, err := manager.clusterClient.apiClient.AddNodes(manager.clusterClient.organization, manager.clusterClient.id, newNodes)
	if err != nil {
		return 0, err
	}
	resultBytes, _ := json.Marshal(result)
	log.Info("AddNodes request response: " + string(resultBytes)) // does _not_ include a reference to the new node
	timeout := 20 * time.Minute
	expiration := time.NewTimer(timeout).C

	delay := 15 * time.Second
	tick := time.NewTicker(delay).C

	var errorResult error

updateLoop:

	for {
		select {
		case <-expiration:
			errorResult = fmt.Errorf("Request timeout expired")
			break updateLoop
		case <-tick:
			manager.Update()
			states := manager.countStates()
			if (*states)["running"] >= target {
				break updateLoop
			}
			if (*states)["draft"]+(*states)["building"]+(*states)["provisioned"]+(*states)["running"] < target {
				errorResult = fmt.Errorf("Not enough nodes in running or pending states to satisfy request for %d", target)
				break updateLoop
			}
			log.Info("Current running count: ", (*states)["running"])
		}
	}
	return (*states)["running"], errorResult
}

// DeleteNodes calls the StackPointCloud API and ensures that the specified nodes
// are in a deleted state.  Returns the number of nodes deleted and an error
// If the deletion count is less than requested but all nodes are in a deleted
// state, then the error will be nil.
func (manager *NodeManager) DeleteNodes(instanceIDs []string) (int, error) {

	var errorMessage []string
	var err error

	manager.Update()
	var nodeKeys []int
	for _, instanceID := range instanceIDs {
		node, ok := manager.GetNode(instanceID)
		if !ok {
			errorMessage = append(errorMessage, fmt.Sprintf("instanceID %s not present", instanceID))
		} else {
			nodeKeys = append(nodeKeys, node.PrimaryKey)
		}
	}

	if len(nodeKeys) == 0 {
		if len(errorMessage) > 0 {
			err = fmt.Errorf(strings.Join(errorMessage, ", "))
		}
		return 0, err
	}

	log.WithFields(log.Fields{
		"nodeDeleteCount": len(nodeKeys),
	}).Info("Deleting nodes")

	var pollNodeKeys []int
	for _, nodePK := range nodeKeys {
		someResponse, err := manager.clusterClient.apiClient.DeleteNode(manager.clusterClient.organization, manager.clusterClient.id, nodePK)
		if err != nil {
			errorMessage = append(errorMessage, err.Error())
		} else {
			pollNodeKeys = append(pollNodeKeys, nodePK)
			log.WithFields(log.Fields{
				"nodeInstanceID": string(someResponse),
			}).Info("Deleting node")
		}
	}

	if len(pollNodeKeys) == 0 {
		if len(errorMessage) > 0 {
			err = fmt.Errorf(strings.Join(errorMessage, ", "))
		}
		return 0, err
	}

	timeout := 10 * time.Minute
	expiration := time.NewTimer(timeout).C

	delay := 15 * time.Second
	tick := time.NewTicker(delay).C

updateLoop:
	for {
		select {
		case <-expiration:
			errorMessage = append(errorMessage, "Request timeout expired")
			break updateLoop
		case <-tick:
			manager.Update()
			for _, nodePK := range pollNodeKeys {
				node, ok := manager.GetNodePK(nodePK)
				if !ok {
					log.Error("XXX programming error")
				}
				if node.State != "deleted" {
					break // out of inner loop, wait some more
				}
			}
			break updateLoop
		}
	}

	var activeDeletionCount int
	for _, nodePK := range pollNodeKeys {
		node, ok := manager.GetNodePK(nodePK)
		if !ok {
			log.Error("XXX programming error")
		}
		if node.State == "deleted" {
			activeDeletionCount++
		}
	}

	if len(errorMessage) > 0 {
		err = fmt.Errorf(strings.Join(errorMessage, ", "))
	}
	return activeDeletionCount, err
}
