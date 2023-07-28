// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of Cilium

package server

import (
	"context"
	"fmt"
	"net"
	"path"
	"sort"
	"time"

	peerpb "github.com/cilium/cilium/api/v1/peer"
	datapath "github.com/cilium/cilium/pkg/datapath"
	v1 "github.com/cilium/cilium/pkg/gke/apis/nodepool/v1"
	"github.com/cilium/cilium/pkg/gke/client/nodepool/clientset/versioned"
	"github.com/cilium/cilium/pkg/gke/client/nodepool/informers/externalversions"
	peertypes "github.com/cilium/cilium/pkg/hubble/peer/types"
	"github.com/cilium/cilium/pkg/lock"
	"github.com/cilium/cilium/pkg/node/addressing"
	"github.com/cilium/cilium/pkg/node/manager"
	"github.com/cilium/cilium/pkg/node/types"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
)

const informerSyncPeriod = 10 * time.Hour

type globalPeerNotifier struct {
	opts Options
	log  logrus.FieldLogger
	manager.Notifier
	nodePoolInformer cache.SharedIndexInformer

	// mux protects fields below
	mux      *lock.Mutex
	handlers []datapath.NodeHandler

	// nodePools is a map of cluster names to map of node pool names to list of nodes.
	nodePools map[string]map[string][]v1.Node

	// nodes is a map of the node's k8s IP to the node itself.
	nodes map[string]types.Node

	// localNodes is a map of the node's IP to the node itself.
	// It is used for nodes received from peer interface.
	localNodes map[string]types.Node
}

func newGlobalPeerNotifier(log logrus.FieldLogger) *globalPeerNotifier {
	return &globalPeerNotifier{
		log:        log,
		mux:        new(lock.Mutex),
		nodePools:  make(map[string]map[string][]v1.Node),
		nodes:      make(map[string]types.Node),
		localNodes: make(map[string]types.Node),
	}
}

// NewGlobalPeerNotifier returns a new instance of globalPeerNotifier which
// watches ABM NodePool custom resources. When change is detected it calls
// methods on registered handlers which are ongoing RPC requests to Hubble peer
// service.
func NewGlobalPeerNotifier(log logrus.FieldLogger, kc *versioned.Clientset, opts Options) (*globalPeerNotifier, error) {
	gp := newGlobalPeerNotifier(log)
	gp.opts = opts
	gp.log.Info("Starting Hubble Global Peer agent")

	nodePoolInformerFactory := externalversions.NewSharedInformerFactory(kc, informerSyncPeriod)
	gp.nodePoolInformer = nodePoolInformerFactory.Baremetal().V1().NodePools().Informer()

	gp.nodePoolInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    gp.nodePoolAdd,
		UpdateFunc: gp.nodePoolUpdate,
		DeleteFunc: gp.nodePoolDelete,
	})
	return gp, nil
}

// Run starts NodePoolInformer which watches NodePool custom resource.
func (gp *globalPeerNotifier) Run(ctx context.Context) {
	gp.nodePoolInformer.Run(ctx.Done())
}

func (gp *globalPeerNotifier) nodePoolAdd(obj interface{}) {
	np, ok := obj.(*v1.NodePool)
	if !ok {
		gp.log.WithField(
			"type", fmt.Sprintf("%T", obj),
		).Error("NodePool to add has an unexpected type")
		return
	}
	if np.Name == "" {
		gp.log.Error("Missing Name from the NodePool add")
		return
	}
	gp.log.WithField("NodePool.Spec", np.Spec).Debug("Processing NodePool add")

	gp.mux.Lock()
	defer gp.mux.Unlock()
	clusterName := np.Spec.ClusterName

	if _, ok := gp.nodePools[clusterName]; !ok {
		gp.nodePools[clusterName] = make(map[string][]v1.Node)
	}
	if _, ok := gp.nodePools[clusterName][np.Name]; ok {
		gp.log.WithField("NodePool.Name", np.Name).Warn("Replacing existing node pool in NodePool add")
	}

	gp.nodePools[clusterName][np.Name] = np.Spec.Nodes
	gp.processNodeChanges()
}

// nodePoolsToMap processes all node pools in an alphabetic order (cluster name
// then node pool name) and creates a map of nodes from it. When duplicate node
// is detected, an older one takes precedence.
func (gp *globalPeerNotifier) nodePoolsToMap() map[string]types.Node {
	curr := make(map[string]types.Node)

	clusterKeys := make([]string, 0, len(gp.nodePools))
	for k := range gp.nodePools {
		clusterKeys = append(clusterKeys, k)
	}
	sort.Strings(clusterKeys)

	for _, clusterKey := range clusterKeys {
		nodePools := gp.nodePools[clusterKey]

		nodePoolKeys := make([]string, 0, len(nodePools))
		for k := range nodePools {
			nodePoolKeys = append(nodePoolKeys, k)
		}
		sort.Strings(nodePoolKeys)

		for _, nodePoolKey := range nodePoolKeys {
			nodePool := nodePools[nodePoolKey]
			for _, node := range nodePool {
				addr := node.GetK8sIP()
				if _, ok := curr[addr]; ok {
					// Do not overwrite nodes.
					continue
				}
				curr[addr] = v1ToNode(clusterKey, addr)
			}
		}
	}
	return curr
}

func (gp *globalPeerNotifier) processNodeChanges() {
	curr := gp.nodePoolsToMap()
	old := gp.nodes

	// Process removed nodes.
	for addr, node := range old {
		if _, ok := curr[addr]; !ok {
			gp.removeNode(node)
		}
	}

	// Process added nodes.
	for addr, node := range curr {
		if _, ok := old[addr]; !ok {
			gp.addNode(node)
		}
	}

	gp.nodes = curr
}

func checkNodeListsEqual(a, b []v1.Node) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Slice(a, func(i, j int) bool { return a[i].Address < a[j].Address })
	sort.Slice(b, func(i, j int) bool { return b[i].Address < b[j].Address })
	for i := range a {
		if a[i].Address != b[i].Address {
			return false
		}
	}
	return true
}

func (gp *globalPeerNotifier) nodePoolUpdate(old, curr interface{}) {
	np, ok := curr.(*v1.NodePool)
	if !ok {
		gp.log.WithField(
			"type", fmt.Sprintf("%T", curr),
		).Error("New NodePool to update has an unexpected type")
		return
	}
	clusterName := np.Spec.ClusterName
	if np.Name == "" {
		gp.log.Error("Missing Name from the NodePool update")
		return
	}

	// Processing relies on internal state instead of contents of old NodePool.
	// Print warnings and keep processing.
	if onp, ok := old.(*v1.NodePool); ok {
		if clusterName != onp.Spec.ClusterName {
			gp.log.WithFields(logrus.Fields{
				"old": onp.Spec.ClusterName,
				"new": clusterName,
			}).Warn("Mismatched cluster names in node pool update")
		}
	} else {
		gp.log.WithField(
			"type", fmt.Sprintf("%T", curr),
		).Warn("Old NodePool to update has an unexpected type")
	}
	gp.log.WithField("NodePool.Spec", np.Spec).Debug("Processing NodePool update")

	gp.mux.Lock()
	defer gp.mux.Unlock()

	if _, ok := gp.nodePools[clusterName]; !ok {
		gp.nodePools[clusterName] = make(map[string][]v1.Node)
	}
	if oldNodes, ok := gp.nodePools[clusterName][np.Name]; !ok {
		gp.log.WithField("NodePool.Name", np.Name).Warn("Adding a new node pool in NodePool update")
	} else if checkNodeListsEqual(oldNodes, np.Spec.Nodes) {
		gp.log.Debug("No changes to nodes list. Ignoring NodePool update")
		return
	}

	gp.nodePools[clusterName][np.Name] = np.Spec.Nodes
	gp.processNodeChanges()
}

func (gp *globalPeerNotifier) nodePoolDelete(obj interface{}) {
	np, ok := obj.(*v1.NodePool)
	if !ok {
		gp.log.WithField(
			"type", fmt.Sprintf("%T", obj),
		).Error("NodePool to delete has an unexpected type")
		return
	}
	if np.Name == "" {
		gp.log.Error("Missing Name from the NodePool delete")
		return
	}
	clusterName := np.Spec.ClusterName
	if clusterName == "" {
		gp.log.Error("Missing ClusterName from the NodePool delete")
		return
	}
	gp.log.WithField("NodePool.Spec", np.Spec).Debug("Processing NodePool delete")

	gp.mux.Lock()
	defer gp.mux.Unlock()

	if _, ok := gp.nodePools[clusterName]; !ok {
		gp.nodePools[clusterName] = make(map[string][]v1.Node)
	}
	if _, ok := gp.nodePools[clusterName][np.Name]; !ok {
		gp.log.WithField("NodePool.Name", np.Name).Warn("Removing a deleted node pool in NodePool delete")
	}

	delete(gp.nodePools[clusterName], np.Name)
	gp.processNodeChanges()
}

func (gp *globalPeerNotifier) removeNode(node types.Node) {
	gp.log.WithField("nodeName", node.Name).Debug("Sending node delete to subscribers")
	for _, handler := range gp.handlers {
		handler.NodeDelete(node)
	}
}

func v1ToNode(clusterName, addr string) types.Node {
	return types.Node{
		Name:    addr,
		Cluster: clusterName,
		IPAddresses: []types.Address{{
			Type: addressing.NodeExternalIP,
			IP:   net.ParseIP(addr),
		}},
	}
}

func (gp *globalPeerNotifier) addNode(node types.Node) {
	gp.log.WithField("nodeName", node.Name).Debug("Sending node add to subscribers")
	for _, handler := range gp.handlers {
		handler.NodeAdd(node)
	}
}

// Subcribe registers handler which will be informed about current list of nodes
// and any future updates to it. It is a part of implementation of Notifier
// interface.
func (gp *globalPeerNotifier) Subscribe(nh datapath.NodeHandler) {
	gp.mux.Lock()
	defer gp.mux.Unlock()

	id := len(gp.handlers)
	gp.log.WithField("ID", id).Info("New subscriber")
	gp.handlers = append(gp.handlers, nh)

	gp.log.WithFields(logrus.Fields{
		"ID":        id,
		"nodeCount": len(gp.nodes),
	}).Info("Sending initial list of nodes to handler")
	for _, node := range gp.nodes {
		gp.log.WithFields(logrus.Fields{
			"ID":       id,
			"nodeName": node.Name,
		}).Debug("Sending initial node info from node pools to handler")
		nh.NodeAdd(node)
	}
	for _, node := range gp.localNodes {
		gp.log.WithFields(logrus.Fields{
			"ID":       id,
			"nodeName": node.Name,
		}).Debug("Sending initial node info from local cluster to handler")
		nh.NodeAdd(node)
	}
}

// Unsubscribe unregisters handler. It is a part of implementation of Notifier
// interface.
func (gp *globalPeerNotifier) Unsubscribe(nh datapath.NodeHandler) {
	gp.mux.Lock()
	defer gp.mux.Unlock()

	for i, handler := range gp.handlers {
		if handler == nh {
			gp.handlers[i] = gp.handlers[len(gp.handlers)-1]
			gp.handlers[len(gp.handlers)-1] = nil
			gp.handlers = gp.handlers[:len(gp.handlers)-1]
			gp.log.WithFields(logrus.Fields{
				"subscriber":      i,
				"subscriberCount": len(gp.handlers),
			}).Info("Unsubscribed handler")
			return
		}
	}
	gp.log.WithField("handler", fmt.Sprintf("%#v", nh)).Warn("Called unsubscribe on handler not on the list")
}

func (gp *globalPeerNotifier) watchNotifications(ctx context.Context, peerClientBuilder peertypes.ClientBuilder) error {
	connectAndProcess := func(ctx context.Context) {
		cl, err := peerClientBuilder.Client(gp.opts.PeerTarget)
		if err != nil {
			gp.log.WithFields(logrus.Fields{
				"error":  err,
				"target": gp.opts.PeerTarget,
			}).Warn("Failed to create peer client for peers synchronization; will try again after the timeout has expired")
			return
		}
		defer cl.Close()
		gp.requestAndProcessNotifications(ctx, cl)
	}

	wait.UntilWithContext(ctx, connectAndProcess, gp.opts.RetryTimeout)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("notify for peer change notification: %w", err)
	}
	return nil
}

func (gp *globalPeerNotifier) requestAndProcessNotifications(ctx context.Context, cl peertypes.Client) {
	client, err := cl.Notify(ctx, &peerpb.NotifyRequest{})
	if err != nil {
		gp.log.WithFields(logrus.Fields{
			"error":             err,
			"connectionTimeout": gp.opts.DialTimeout,
		}).Warn("Failed to create peer notify client for peers change notification; will try again after the timeout has expired")
		return
	}
	for {
		cn, err := client.Recv()
		if err != nil {
			gp.log.WithFields(logrus.Fields{
				"error":             err,
				"connectionTimeout": gp.opts.DialTimeout,
			}).Warn("Error while receiving peer change notification; will try again after the timeout has expired")
			return
		}
		gp.log.WithField("changeNotification", cn).Info("Received peer change notification")
		gp.processChangeNotification(cn)
	}
}

func (gp *globalPeerNotifier) processChangeNotification(cn *peerpb.ChangeNotification) {
	gp.mux.Lock()
	defer gp.mux.Unlock()

	p := peertypes.FromChangeNotification(cn)
	addr := p.Address.(*net.TCPAddr).IP.String()
	node := types.Node{
		Name:    path.Base(p.Name),
		Cluster: gp.opts.ClusterName,
		IPAddresses: []types.Address{{
			Type: addressing.NodeExternalIP,
			IP:   p.Address.(*net.TCPAddr).IP,
		}},
	}
	switch cn.GetType() {
	case peerpb.ChangeNotificationType_PEER_ADDED:
		gp.localNodes[addr] = node
		gp.addNode(node)
	case peerpb.ChangeNotificationType_PEER_DELETED:
		gp.removeNode(node)
		delete(gp.localNodes, addr)
	case peerpb.ChangeNotificationType_PEER_UPDATED:
		gp.log.WithField("nodeAddr", addr).Error("Unhandled PEER_UPDATE")
	}
}
