// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package server

import (
	"context"
	"testing"
	"time"

	peerpb "github.com/cilium/cilium/api/v1/peer"
	"github.com/cilium/cilium/pkg/datapath/fake"
	v1 "github.com/cilium/cilium/pkg/gke/apis/nodepool/v1"
	"github.com/cilium/cilium/pkg/node/types"

	"github.com/google/go-cmp/cmp"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var (
	allNodes = []v1.Node{
		{
			Address: "10.0.0.1",
		},
		{
			Address: "10.0.0.2",
			K8sIP:   pointer.String("10.0.0.3"),
		},
		{
			Address: "10.0.0.4",
		},
	}
	updateNodes = allNodes[:2]

	baseNodePool = &v1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "base-cluster-nodepool",
		},
		Spec: v1.NodePoolSpec{
			ClusterName: "base-cluster-name",
			Nodes:       allNodes,
		},
	}
	updateNodePool = &v1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "base-cluster-nodepool",
		},
		Spec: v1.NodePoolSpec{
			ClusterName: "base-cluster-name",
			Nodes:       updateNodes,
		},
	}
	secondNodePool = &v1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "base-cluster-nodepool-second",
		},
		Spec: v1.NodePoolSpec{
			ClusterName: "base-cluster-name",
			Nodes:       allNodes[2:],
		},
	}
	emptyNodePool = &v1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "base-cluster-nodepool",
		},
		Spec: v1.NodePoolSpec{
			ClusterName: "base-cluster-name",
		},
	}

	differentNodePool = &v1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "different-cluster-nodepool-1",
		},
		Spec: v1.NodePoolSpec{
			ClusterName: "different-cluster-name",
			Nodes: []v1.Node{
				{Address: "10.1.0.1"},
			},
		},
	}
	differentNodePoolDuplicateNode = &v1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "different-cluster-nodepool",
		},
		Spec: v1.NodePoolSpec{
			ClusterName: "different-cluster-name",
			Nodes:       allNodes[:1],
		},
	}

	initialNodes = map[string]types.Node{
		"10.0.0.1": v1ToNode(baseNodePool.Spec.ClusterName, allNodes[0].Address),
		"10.0.0.3": v1ToNode(baseNodePool.Spec.ClusterName, *allNodes[1].K8sIP),
		"10.0.0.4": v1ToNode(baseNodePool.Spec.ClusterName, allNodes[2].Address),
	}
	twoPoolsNodes = map[string]types.Node{
		"10.0.0.1": v1ToNode(baseNodePool.Spec.ClusterName, allNodes[0].Address),
		"10.0.0.3": v1ToNode(baseNodePool.Spec.ClusterName, *allNodes[1].K8sIP),
		"10.0.0.4": v1ToNode(baseNodePool.Spec.ClusterName, allNodes[2].Address),
	}
	afterNodeUpdate = map[string]types.Node{
		"10.0.0.1": v1ToNode(baseNodePool.Spec.ClusterName, allNodes[0].Address),
		"10.0.0.3": v1ToNode(baseNodePool.Spec.ClusterName, *allNodes[1].K8sIP),
	}
	afterNodeUpdateWithDifferentCluster = map[string]types.Node{
		"10.0.0.1": v1ToNode(baseNodePool.Spec.ClusterName, allNodes[0].Address),
		"10.0.0.3": v1ToNode(baseNodePool.Spec.ClusterName, *allNodes[1].K8sIP),
		"10.1.0.1": v1ToNode(differentNodePool.Spec.ClusterName, "10.1.0.1"),
	}
	afterNodeDelete = map[string]types.Node{}

	handlerInitialNodes     = convertToHandlerNodes(initialNodes)
	handlerAfterUpdateNodes = convertToHandlerNodes(afterNodeUpdate)
)

func convertToHandlerNodes(src map[string]types.Node) (dst map[string]types.Node) {
	dst = make(map[string]types.Node)
	for _, node := range src {
		dst[node.Name] = node
	}
	return
}

func TestGlobalPeerNotifier_Add(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	gp.nodePoolAdd(baseNodePool)
	if diff := cmp.Diff(initialNodes, gp.nodes); diff != "" {
		t.Fatalf("Mismatch after processing initial NodePoolAdd (-want, +got):\n%v", diff)
	}
}

func TestGlobalPeerNotifier_UpdateTwiceHasNoEffect(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)
	for k, v := range initialNodes {
		gp.nodes[k] = v
	}

	gp.nodePoolUpdate(baseNodePool, updateNodePool)
	if diff := cmp.Diff(afterNodeUpdate, gp.nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}

	gp.nodePoolUpdate(baseNodePool, updateNodePool)
	if diff := cmp.Diff(afterNodeUpdate, gp.nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}
}

func TestGlobalPeerNotifier_Update(t *testing.T) {
	testcases := []struct {
		name      string
		before    *v1.NodePool
		after     map[string]types.Node
		old, curr *v1.NodePool
	}{
		{
			name:   "Add new node (with correct old entry)",
			before: baseNodePool,
			old:    baseNodePool,
			curr:   updateNodePool,
			after:  afterNodeUpdate,
		},
		{
			name:   "Add new node (without old entry)",
			before: baseNodePool,
			old:    emptyNodePool,
			curr:   updateNodePool,
			after:  afterNodeUpdate,
		},
		{
			name:   "Duplicate node from different cluster has no effect",
			before: updateNodePool,
			old:    differentNodePool,
			curr:   differentNodePoolDuplicateNode,
			after:  afterNodeUpdate,
		},
		{
			name:   "Unexpected cluster name change is still processed",
			before: updateNodePool,
			old:    emptyNodePool,
			curr:   differentNodePool,
			after:  afterNodeUpdateWithDifferentCluster,
		},
		{
			name:   "Cluster can consist of multiple node pools",
			before: updateNodePool,
			old:    emptyNodePool,
			curr:   secondNodePool,
			after:  twoPoolsNodes,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			logger, _ := test.NewNullLogger()
			gp := newGlobalPeerNotifier(logger)

			// Populate NodePools and nodes.
			gp.nodePoolAdd(tc.before)

			gp.nodePoolUpdate(tc.old, tc.curr)
			if diff := cmp.Diff(tc.after, gp.nodes); diff != "" {
				t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
			}
		})
	}
}

func TestGlobalPeerNotifier_Delete(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	// Populate NodePools and nodes.
	for _, np := range []*v1.NodePool{updateNodePool, differentNodePool} {
		gp.nodePoolAdd(np)
	}

	gp.nodePoolDelete(differentNodePool)
	if diff := cmp.Diff(afterNodeUpdate, gp.nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolDelete (-want, +got):\n%v", diff)
	}

	gp.nodePoolDelete(baseNodePool)
	if diff := cmp.Diff(afterNodeDelete, gp.nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolDelete (-want, +got):\n%v", diff)
	}
}

func TestGlobalPeerNotifier_SubscribedFromStart(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	handler := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	gp.Subscribe(handler)

	gp.nodePoolAdd(baseNodePool)
	if diff := cmp.Diff(handlerInitialNodes, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing initial NodePoolAdd (-want, +got):\n%v", diff)
	}

	gp.nodePoolUpdate(baseNodePool, updateNodePool)
	if diff := cmp.Diff(handlerAfterUpdateNodes, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}

	gp.nodePoolDelete(baseNodePool)
	if diff := cmp.Diff(afterNodeDelete, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolDelete (-want, +got):\n%v", diff)
	}
}

func hasNodeCountFactory(nh *fake.FakeNodeHandler) func(count int) func() bool {
	return func(count int) func() bool {
		return func() bool { return count == len(nh.Nodes) }
	}
}

func TestGlobalPeerNotifier_SubscribedAfterNew(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	gp.nodePoolAdd(baseNodePool)

	handler := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	hasNodeCount := hasNodeCountFactory(handler)
	gp.Subscribe(handler)
	assert.Eventually(t, hasNodeCount(3), time.Minute, 10*time.Millisecond)
	if diff := cmp.Diff(handlerInitialNodes, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing initial NodePoolAdd (-want, +got):\n%v", diff)
	}

	gp.nodePoolUpdate(baseNodePool, updateNodePool)
	if diff := cmp.Diff(handlerAfterUpdateNodes, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}

	gp.nodePoolDelete(baseNodePool)
	assert.Empty(t, handler.Nodes)
}

func TestGlobalPeerNotifier_SubscribedAfterUpdate(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	gp.nodePoolAdd(baseNodePool)
	gp.nodePoolUpdate(baseNodePool, updateNodePool)

	handler := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	hasNodeCount := hasNodeCountFactory(handler)
	gp.Subscribe(handler)
	assert.Eventually(t, hasNodeCount(2), time.Minute, 10*time.Millisecond)
	if diff := cmp.Diff(handlerAfterUpdateNodes, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}

	gp.nodePoolDelete(baseNodePool)
	assert.Empty(t, handler.Nodes)
}

func TestGlobalPeerNotifier_TwoHandlers(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	gp.nodePoolAdd(baseNodePool)

	handlerA := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	hasNodeCountA := hasNodeCountFactory(handlerA)
	gp.Subscribe(handlerA)
	assert.Eventually(t, hasNodeCountA(3), time.Minute, 10*time.Millisecond)
	if diff := cmp.Diff(handlerInitialNodes, handlerA.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing initial NodePoolAdd (-want, +got):\n%v", diff)
	}

	gp.nodePoolUpdate(baseNodePool, updateNodePool)
	handlerB := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	hasNodeCountB := hasNodeCountFactory(handlerB)
	gp.Subscribe(handlerB)
	assert.Equal(t, 2, len(handlerA.Nodes))
	assert.Eventually(t, hasNodeCountB(2), time.Minute, 10*time.Millisecond)
	if diff := cmp.Diff(handlerAfterUpdateNodes, handlerA.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}
	if diff := cmp.Diff(handlerAfterUpdateNodes, handlerB.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}

	gp.nodePoolDelete(baseNodePool)
	assert.Empty(t, handlerA.Nodes)
	assert.Empty(t, handlerB.Nodes)
}

func TestGlobalPeerNotifier_Unsubscribe(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	gp.nodePoolAdd(baseNodePool)

	handler := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	hasNodeCount := hasNodeCountFactory(handler)
	gp.Subscribe(handler)
	assert.Eventually(t, hasNodeCount(3), time.Minute, 10*time.Millisecond)
	if diff := cmp.Diff(handlerInitialNodes, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing initial NodePoolAdd (-want, +got):\n%v", diff)
	}

	gp.nodePoolUpdate(baseNodePool, updateNodePool)
	if diff := cmp.Diff(handlerAfterUpdateNodes, handler.Nodes); diff != "" {
		t.Fatalf("Mismatch after processing NodePoolUpdate (-want, +got):\n%v", diff)
	}

	gp.Unsubscribe(handler)

	gp.nodePoolDelete(baseNodePool)
	assert.Equal(t, 2, len(handler.Nodes))
}

func TestGlobalPeerNotifier_UnsubscribeNonExisting(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	handler := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	gp.Unsubscribe(handler)
	assert.Empty(t, gp.handlers)
}

func TestGlobalPeerNotifier_WithPeerNotifications(t *testing.T) {
	logger, _ := test.NewNullLogger()
	gp := newGlobalPeerNotifier(logger)

	handler := fake.NewNodeHandler().(*fake.FakeNodeHandler)
	gp.Subscribe(handler)

	cn := make(chan *peerpb.ChangeNotification)
	defer close(cn)
	peerClientBuilder := newSpyPeerClientBuilder(cn)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go gp.watchNotifications(ctx, peerClientBuilder)

	hasNodeCount := func(count int) func() bool {
		return func() bool { return count == len(handler.Nodes) }
	}

	t.Log("Add first peer")
	cn <- &peerpb.ChangeNotification{
		Name:    "192.0.1.1",
		Address: "192.0.1.1",
		Type:    peerpb.ChangeNotificationType_PEER_ADDED,
	}
	assert.Eventually(t, hasNodeCount(1), time.Minute, 10*time.Millisecond)
	wantOne := map[string]types.Node{"192.0.1.1": v1ToNode("", "192.0.1.1")}
	if diff := cmp.Diff(handler.Nodes, wantOne); diff != "" {
		t.Fatalf("Mismatch after processing PEER_ADDED (-want, +got):\n%v", diff)
	}

	t.Log("Add first peer again and another one")
	cn <- &peerpb.ChangeNotification{
		Name:    "192.0.1.1",
		Address: "192.0.1.1",
		Type:    peerpb.ChangeNotificationType_PEER_ADDED,
	}
	cn <- &peerpb.ChangeNotification{
		Name:    "192.0.1.2",
		Address: "192.0.1.2",
		Type:    peerpb.ChangeNotificationType_PEER_ADDED,
	}
	assert.Eventually(t, hasNodeCount(2), time.Minute, 10*time.Millisecond)
	wantTwo := map[string]types.Node{
		"192.0.1.1": v1ToNode("", "192.0.1.1"),
		"192.0.1.2": v1ToNode("", "192.0.1.2"),
	}
	if diff := cmp.Diff(handler.Nodes, wantTwo); diff != "" {
		t.Fatalf("Mismatch after processing PEER_ADDED again (-want, +got):\n%v", diff)
	}

	t.Log("Remove second peer")
	cn <- &peerpb.ChangeNotification{
		Name:    "192.0.1.2",
		Address: "192.0.1.2",
		Type:    peerpb.ChangeNotificationType_PEER_DELETED,
	}
	assert.Eventually(t, hasNodeCount(1), time.Minute, 10*time.Millisecond)
	if diff := cmp.Diff(handler.Nodes, wantOne); diff != "" {
		t.Fatalf("Mismatch after processing PEER_DELETED (-want, +got):\n%v", diff)
	}

	lateHandler := fake.NewNodeHandler()
	gp.Subscribe(lateHandler)
	hasLateNodeCount := func(count int) func() bool {
		return func() bool { return count == len(handler.Nodes) }
	}
	assert.Eventually(t, hasLateNodeCount(1), time.Minute, 10*time.Millisecond)

	t.Log("Remove second peer again, first one, and nonexistent")
	cn <- &peerpb.ChangeNotification{
		Name:    "192.0.1.2",
		Address: "192.0.1.2",
		Type:    peerpb.ChangeNotificationType_PEER_DELETED,
	}
	cn <- &peerpb.ChangeNotification{
		Name:    "192.0.1.1",
		Address: "192.0.1.1",
		Type:    peerpb.ChangeNotificationType_PEER_DELETED,
	}
	cn <- &peerpb.ChangeNotification{
		Name:    "192.0.1.0",
		Address: "192.0.1.0",
		Type:    peerpb.ChangeNotificationType_PEER_DELETED,
	}
	assert.Eventually(t, hasNodeCount(0), time.Minute, 10*time.Millisecond)
}
