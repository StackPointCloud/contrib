/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package spc

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/StackPointCloud/stackpoint-sdk-go"
	"github.com/golang/glog"
	"k8s.io/contrib/cluster-autoscaler/cloudprovider"
	kube_api "k8s.io/kubernetes/pkg/api"
)

// CreateClusterClient creates a stackpointio.ClusterClient from environment
// variables {CLUSTER_API_TOKEN, SPC_BASE_API_URL, ORGANIZATION_ID, CLUSTER_ID}
func CreateClusterClient() (*stackpointio.ClusterClient, error) {

	token := os.Getenv("CLUSTER_API_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("Environment variable CLUSTER_API_TOKEN not defined")
	}
	endpoint := os.Getenv("SPC_API_BASE_URL")
	if endpoint == "" {
		return nil, fmt.Errorf("Environment variable SPC_API_BASE_URL not defined")
	}
	apiClient := stackpointio.NewClient(token, endpoint)
	glog.V(5).Infof("Using stackpoint io api server [%s]", endpoint)

	organizationID := os.Getenv("ORGANIZATION_ID")
	clusterID := os.Getenv("CLUSTER_ID")
	glog.V(5).Infof("Using stackpoint organization [%s], cluster [%s]", organizationID, clusterID)

	orgPk, err := strconv.Atoi(organizationID)
	if err != nil {
		return nil, fmt.Errorf("Bad environment variable for organizationID [%s]", organizationID)
	}
	clusterPk, err := strconv.Atoi(clusterID)
	if err != nil {
		return nil, fmt.Errorf("Bad environment variable for clusterID [%s]", clusterID)
	}

	clusterClient := stackpointio.CreateClusterClient(orgPk, clusterPk, apiClient)
	return clusterClient, nil
}

// SpcNodeGroup implements NodeGroup
type SpcNodeGroup struct {
	id      string
	maxSize int
	minSize int
	// currentSize int
	// targetSize  int
	manager *stackpointio.NodeManager
}

// MaxSize returns maximum size of the node group.
func (sng *SpcNodeGroup) MaxSize() int {
	return sng.maxSize
}

// MinSize returns minimum size of the node group.
func (sng *SpcNodeGroup) MinSize() int {
	return sng.minSize
}

// // Size returns the current size of the node group.
// func (sng *SpcNodeGroup) Size() int {
// 	return sng.currentSize
// }

// TargetSize returns the current target size of the node group. It is possible that the
// number of nodes in Kubernetes is different at the moment but should be equal
// to Size() once everything stabilizes (new nodes finish startup and registration or
// removed nodes are deleted completely)
func (sng *SpcNodeGroup) TargetSize() (int, error) {
	return sng.manager.Size(), nil
}

// IncreaseSize increases the size of the node group. To delete a node you need
// to explicitly name it and use DeleteNode. This function should wait until
// node group size is updated.
func (sng *SpcNodeGroup) IncreaseSize(delta int) error {
	_, err := sng.manager.IncreaseSize(delta)
	return err
}

// DeleteNodes deletes nodes from this node group. Error is returned either on
// failure or if the given node doesn't belong to this node group. This function
// should wait until node group size is updated.
func (sng *SpcNodeGroup) DeleteNodes(nodes []*kube_api.Node) error {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		instanceID, err := instanceIdForNode(node)
		if err != nil {
			glog.V(2).Info(err.Error())
		} else {
			ids = append(ids, instanceID)
		}
	}
	_, err := sng.manager.DeleteNodes(ids)
	return err
}

func instanceIdForNode(node *kube_api.Node) (string, error) {
	labels := node.ObjectMeta.Labels
	instanceID, ok := labels["stackpoint.io/instance_id"]
	if !ok {
		var errorResp error
		hostname, reallyOK := labels["kubernetes.io/hostname"]
		if reallyOK {
			errorResp = fmt.Errorf("Unable to find stackpointio instance_id label for node [%s]", hostname)
		} else {
			errorResp = fmt.Errorf("Unable to find stackpointio instance_id label for a nodes without 'kubernetes.io/hostname' label")
		}
		return "", errorResp
	}
	return instanceID, nil
}

// Id returns an unique identifier of the node group.
func (sng *SpcNodeGroup) Id() string {
	return sng.id
}

// Debug returns a string containing all information regarding this node group.
func (sng *SpcNodeGroup) Debug() string {
	var msg string
	target, err := sng.TargetSize()
	if err != nil {
		msg = err.Error()
	}
	return fmt.Sprintf("%s (%d:%d) (%d) %s", sng.Id(), sng.MinSize(), sng.MaxSize(), target, msg)
}

// SpcCloudProvider implements CloudProvider
type SpcCloudProvider struct {
	spcClient  *stackpointio.ClusterClient
	nodeGroups []*SpcNodeGroup
}

// BuildSpcCloudProvider builds CloudProvider implementation for stackpointio.
func BuildSpcCloudProvider(spcClient *stackpointio.ClusterClient, specs []string) (*SpcCloudProvider, error) {
	if spcClient == nil {
		return nil, fmt.Errorf("stackpointio.ClusterClient is nil")
	}
	if len(specs) == 0 {
		glog.V(5).Info("No node groups specified, faking one now")
		//return nil, fmt.Errorf("No node groups specified, must specify one")
		specs = []string{"2:4:t2.large"}
	} else if len(specs) > 1 {
		return nil, fmt.Errorf("Multiple node groups not supported at this time")
	}
	spc := &SpcCloudProvider{
		spcClient:  spcClient,
		nodeGroups: make([]*SpcNodeGroup, 0),
	}
	if err := spc.addNodeGroup(specs[0]); err != nil {
		return nil, err
	}
	return spc, nil
}

// addNodeGroup adds node group defined in string spec. Format:
// minNodes:maxNodes:typeofNode
func (spc *SpcCloudProvider) addNodeGroup(spec string) error {
	glog.V(5).Infof("Node group specification: %s", spec)
	minSize, maxSize, nodeType, err := tokenizeNodeGroupSpec(spec)
	if err != nil {
		return err
	}
	if spc.spcClient == nil {
		return fmt.Errorf("Something's gone terribly wrong, spc.spcClient is nil")
	}
	manager := stackpointio.CreateNodeManager(nodeType, spc.spcClient)
	err = manager.Update()
	if err != nil {
		return err
	}
	newNodeGroup := &SpcNodeGroup{
		id:      "default",
		minSize: minSize,
		maxSize: maxSize,
		manager: manager,
	}
	spc.nodeGroups = append(spc.nodeGroups, newNodeGroup)
	return nil
}

func tokenizeNodeGroupSpec(spec string) (minSize, maxSize int, nodeSizeProviderString string, err error) {

	tokens := strings.SplitN(spec, ":", 3)
	if len(tokens) != 3 {
		return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("bad node configuration: %s", spec)
	}

	if size, err := strconv.Atoi(tokens[0]); err == nil {
		if size <= 0 {
			return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("min size must be >= 1")
		}
		minSize = size
	} else {
		return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("failed to set min size: %s, expected integer", tokens[0])
	}

	if size, err := strconv.Atoi(tokens[1]); err == nil {
		if size < minSize {
			return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("max size must be greater or equal to min size")
		}
		maxSize = size
	} else {
		return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("failed to set max size: %s, expected integer", tokens[1])
	}

	nodeSizeProviderString = tokens[2] // TODO validate + error checking

	return minSize, maxSize, nodeSizeProviderString, nil
}

// Name returns name of the cloud provider.
func (spc *SpcCloudProvider) Name() string {
	return "spc"
}

// NodeGroups returns all node groups configured for this cloud provider.
func (spc *SpcCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	result := make([]cloudprovider.NodeGroup, 0, len(spc.nodeGroups))
	for _, nodeGroups := range spc.nodeGroups {
		result = append(result, nodeGroups)
	}
	return result
}

// NodeGroupForNode returns the node group for the given node, nil if the node
// should not be processed by cluster autoscaler, or non-nil error if such
// occurred.
func (spc *SpcCloudProvider) NodeGroupForNode(node *kube_api.Node) (cloudprovider.NodeGroup, error) {
	instanceID, error := instanceIdForNode(node)
	if error != nil {
		glog.V(2).Infof("Error node not manageable %s, can't get instance_id", node.Spec.ExternalID)
		return nil, nil
	}
	glog.V(5).Infof("Looking up node group for %s", instanceID)
	spc.nodeGroups[0].manager.Update()
	spcNode, ok := spc.nodeGroups[0].manager.GetNode(instanceID)
	if !ok {
		glog.V(2).Infof("Could not find node group for %s", instanceID)
		return nil, fmt.Errorf("Could not find node group for %s", instanceID)
	}
	glog.V(5).Infof("Found spc node (%d:%s) for instance_id %s", spcNode.PrimaryKey, spcNode.Name, instanceID)
	return spc.nodeGroups[0], nil
}
