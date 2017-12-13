package spc

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/StackPointCloud/stackpoint-sdk-go"

	"github.com/golang/glog"
)

const (
	spcAPIRequestLimit     time.Duration = 60 * time.Second
	scalingPollingInterval time.Duration = 60 * time.Second
	scalingTimeout         time.Duration = 20 * time.Minute
)

// ClusterClient represents an api interface
type ClusterClient interface {

	// retrieve all nodes for a cluster
	getNodes() ([]stackpointio.Node, error)

	// add a node to the cluster
	addNodes(requestNodes stackpointio.NodeAdd) ([]stackpointio.Node, error)

	// delete a node pby primate key
	deleteNode(nodePK int) ([]byte, error)

	// get the id of the organization that owns the cluster
	getOrganization() int

	// get the id of the cluster
	getID() int
}

// StackpointClusterClient is a StackPointCloud API client for a particular cluster
type StackpointClusterClient struct {
	organization int
	id           int
	apiClient    *stackpointio.APIClient
}

// CreateClusterClient creates a ClusterClient from environment
// variables {CLUSTER_API_TOKEN, SPC_BASE_API_URL, ORGANIZATION_ID, CLUSTER_ID}
func CreateClusterClient() (*StackpointClusterClient, error) {

	token := string("")
	endpoint := string("")
	organizationID := string("")
	clusterID := string("")

	//  // to read from a configuration ...
	// if configReader != nil {
	// 	var cfg Config
	// 	if err := gcfg.ReadInto(&cfg, configReader); err != nil {
	// 		glog.Errorf("Couldn't read config: %v", err)
	// 		return nil, err
	// 	}
	// 	token = cfg.APIToken
	// 	endpoint = cfg.APIEndpoint
	// 	organizationID = cfg.OrganizationID
	// 	clusterID = cfg.ClusterID
	//
	// } else {

	token = os.Getenv("CLUSTER_API_TOKEN")
	endpoint = os.Getenv("SPC_API_BASE_URL")
	organizationID = os.Getenv("ORGANIZATION_ID")
	clusterID = os.Getenv("CLUSTER_ID")

	// }

	if token == "" {
		return nil, fmt.Errorf("Environment variable CLUSTER_API_TOKEN not defined")
	}
	if endpoint == "" {
		return nil, fmt.Errorf("Environment variable SPC_API_BASE_URL not defined")
	}
	orgPk, err := strconv.Atoi(organizationID)
	if err != nil {
		return nil, fmt.Errorf("Bad environment variable for organizationID [%s]", organizationID)
	}
	clusterPk, err := strconv.Atoi(clusterID)
	if err != nil {
		return nil, fmt.Errorf("Bad environment variable for clusterID [%s]", clusterID)
	}

	apiClient := stackpointio.NewClient(token, endpoint)
	glog.V(5).Infof("Using stackpoint io api server [%s]", endpoint)
	glog.V(5).Infof("Using stackpoint organization [%s], cluster [%s]", organizationID, clusterID)

	clusterClient := &StackpointClusterClient{
		organization: orgPk,
		id:           clusterPk,
		apiClient:    apiClient,
	}
	return clusterClient, nil
}

func (cClient *StackpointClusterClient) getOrganization() int { return cClient.organization }

func (cClient *StackpointClusterClient) getID() int { return cClient.id }

func (cClient *StackpointClusterClient) getNodes() ([]stackpointio.Node, error) {
	return cClient.apiClient.GetNodes(cClient.organization, cClient.id)
}

func (cClient *StackpointClusterClient) addNodes(requestNodes stackpointio.NodeAdd) ([]stackpointio.Node, error) {
	newNodes, err := cClient.apiClient.AddNodes(cClient.organization, cClient.id, requestNodes)
	if err != nil {
		return nil, err // make([]stackpointio.Node, 0), err
	}
	return newNodes, nil
}

func (cClient *StackpointClusterClient) deleteNode(nodePK int) ([]byte, error) {
	someResponse, err := cClient.apiClient.DeleteNode(cClient.organization, cClient.id, nodePK)
	if err != nil {
		return nil, err
	}
	return someResponse, nil
}

// NodeManager has a set of nodes and can add or delete them via the StackPointCloud API
type NodeManager struct {
	clusterClient      *StackpointClusterClient
	nodes              map[string]stackpointio.Node
	apiRequestInterval time.Duration
	lastAPIRequestTime time.Time
}

// CreateNodeManager creates a NodeManager
func CreateNodeManager(cluster *StackpointClusterClient) NodeManager {
	manager := NodeManager{
		clusterClient:      cluster,
		nodes:              make(map[string]stackpointio.Node, 0),
		apiRequestInterval: spcAPIRequestLimit,
	}
	manager.Update()
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

func (manager *NodeManager) addNode(node stackpointio.Node) int {
	manager.nodes[node.InstanceID] = node
	return len(manager.nodes)
}

// Nodes returns the  complete set of stackpointio node instanceIDs
func (manager *NodeManager) Nodes() ([]string, error) {
	var keys []string
	for k := range manager.nodes {
		keys = append(keys, k)
	}
	return keys, nil
}

// NodesForGroupID returns a set of stackpointio node instanceIDs for a given node group
func (manager *NodeManager) NodesForGroupID(group string) ([]string, error) {
	var nodeIDs []string
	glog.V(5).Infof("looking for nodes for %s", group)
	if manager.nodes == nil {
		return nil, fmt.Errorf("node list is nil")
	}
	for _, node := range manager.nodes {
		glog.V(5).Infof("     checking node %s:%s:%s (%s)", node.Name, node.InstanceID, node.PrivateIP, node.Group)
		if node.Group == group {
			nodeIDs = append(nodeIDs, node.InstanceID)
		}
	}
	return nodeIDs, nil
}

// GetNode returns a Node identified by the stackpointio instanceID
func (manager *NodeManager) GetNode(instanceID string) (stackpointio.Node, bool) {
	node, ok := manager.nodes[instanceID]
	return node, ok
}

// GetNodePK returns a Node identified by the stackpointio primaryKey
func (manager *NodeManager) GetNodePK(nodePK int) (stackpointio.Node, bool) {
	for _, node := range manager.nodes {
		if node.PrimaryKey == nodePK {
			return node, true
		}
	}
	return stackpointio.Node{}, false
}

// Update refreshes the state of the current nodes in the clusterClient
func (manager *NodeManager) Update() error {
	glog.V(5).Infof("Updating cluster info, organizationID %d, clusterID %d", manager.clusterClient.getOrganization(), manager.clusterClient.getID())
	timepoint := time.Now()
	if timepoint.Sub(manager.lastAPIRequestTime) < manager.apiRequestInterval {
		return nil
	}
	manager.lastAPIRequestTime = timepoint

	clusterNodes, err := manager.clusterClient.getNodes()
	if err != nil {
		return err
	}
	for _, clusterNode := range clusterNodes {
		localNode, ok := manager.nodes[clusterNode.InstanceID]
		if ok {
			if localNode.State != clusterNode.State {
				glog.V(5).Infof("Node state change, nodeID %s, oldState %s, newState %s", clusterNode.InstanceID, localNode.State, clusterNode.State)
			}
		} else {
			glog.V(5).Infof("New node found, nodeID %s, newState %s", clusterNode.InstanceID, clusterNode.State)
		}
		manager.nodes[clusterNode.InstanceID] = clusterNode
	}
	if len(clusterNodes) < len(manager.nodes) {
		glog.V(2).Infof("Remote node count is too small, remote %d vs. local %d", len(clusterNodes), len(manager.nodes))
		reconciliationSet := make(map[string]int)
		for index, clusterNode := range clusterNodes {
			reconciliationSet[clusterNode.InstanceID] = index
		}
		for instanceID := range manager.nodes {
			_, ok := reconciliationSet[instanceID]
			if !ok {
				glog.V(2).Info("Local node does not exist in cluster, removing nodeID %s", instanceID)
				delete(manager.nodes, instanceID)
			}
		}

	}
	return nil
}

// IncreaseSize adds nodes to the manager and to the cluster, waits until
// the addition is complete.  Returns the count of running nodes.
func (manager *NodeManager) IncreaseSize(additional int, nodeType string, groupName string) (int, error) {
	manager.Update()
	states := manager.countStates()

	requestNodes := stackpointio.NodeAdd{
		Size:  nodeType,
		Count: additional,
		Group: groupName,
	}

	newNodes, err := manager.clusterClient.addNodes(requestNodes)
	if err != nil {
		return 0, err
	}
	for _, node := range newNodes {
		glog.V(5).Infof("AddNodes response {instance_id: %s, state: %s}", node.InstanceID, node.State)
		if node.Group != requestNodes.Group {
			glog.Errorf("AddNodes instance_id: %s is in group [%s] not group [%s]", node.InstanceID, node.Group, groupName)
		}
	}
	//if newNode.Group !=

	expiration := time.NewTimer(scalingTimeout).C
	tick := time.NewTicker(scalingPollingInterval).C

	var errorResult error

updateLoop:

	for {
		select {
		case <-expiration:
			errorResult = fmt.Errorf("Request timeout expired")
			break updateLoop
		case <-tick:
			manager.Update()
			completed := true
			for _, requestNode := range newNodes {
				currentNode, found := manager.GetNode(requestNode.InstanceID)
				if !found {
					glog.Errorf("AddNodes instance_id [%s] not found in current lookup", currentNode.InstanceID)
					break
				}
				glog.V(5).Infof("Checking node {instance_id: %s, state: %s}", currentNode.InstanceID, currentNode.State)
				if currentNode.State != "running" {
					completed = false
				}
			}
			if completed {
				break updateLoop
			}
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

	var pollNodeKeys []int
	for _, nodePK := range nodeKeys {
		someResponse, someErr := manager.clusterClient.deleteNode(nodePK)
		if someErr != nil {
			errorMessage = append(errorMessage, someErr.Error())
		} else {
			pollNodeKeys = append(pollNodeKeys, nodePK)
			glog.V(2).Infof("Deleting node nodeInstanceID %s", string(someResponse))
		}
	}

	if len(pollNodeKeys) == 0 {
		if len(errorMessage) > 0 {
			err = fmt.Errorf(strings.Join(errorMessage, ", "))
		}
		return 0, err
	}

	expiration := time.NewTimer(scalingTimeout).C
	tick := time.NewTicker(scalingPollingInterval).C

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
					glog.Errorf("Unexpected value of stackpoint node id in polling list [%d]", nodePK)
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
			glog.Errorf("Unexpected value of stackpoint node id in polling list [%d]", nodePK)
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
