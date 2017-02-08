package spc

//
// // FakeClusterClient is a test fake
// type FakeClusterClient struct {
// 	organization int
// 	id           int
// 	//	apiClient    *stackpointio.APIClient
// }
//
// func (cClient FakeClusterClient) getOrganization() int { return cClient.organization }
//
// func (cClient FakeClusterClient) getID() int { return cClient.id }
//
// func (cClient FakeClusterClient) getNodes() ([]stackpointio.Node, error) {
// 	return []stackpointio.Node{}, nil
// }
//
// func (cClient FakeClusterClient) addNodes(requestNodes stackpointio.NodeAdd) ([]stackpointio.Node, error) {
// 	newNodes := make([]stackpointio.Node, requestNodes.Count)
// 	for i := range newNodes {
// 		newNodes[i] = stackpointio.Node{
// 			PrimaryKey: i,
// 			Name:       string(i),
// 			ClusterID:  0,
// 			InstanceID: "test",
// 			Role:       "-",
// 			Group:      "autp",
// 			PrivateIP:  "10.0.0.0",
// 			PublicIP:   "10.0.0.0",
// 			Platform:   "-",
// 			Image:      "-",
// 			Location:   "-",
// 			Size:       "-",
// 			State:      "building",
// 			Created:    time.Now(),
// 			Updated:    time.Now(),
// 		}
// 	}
// 	return newNodes, nil
// }
//
// func (cClient FakeClusterClient) deleteNode(nodePK int) ([]byte, error) {
// 	return []byte{}, nil
// }
//
// func createTestNodeManager(nodeType string) *NodeManager {
// 	client := FakeClusterClient{0, 1}
// 	manager := &NodeManager{
// 		NodeType:      nodeType,
// 		clusterClient: client,
// 		nodes:         make(map[string]stackpointio.Node),
// 	}
// 	return manager
// }
