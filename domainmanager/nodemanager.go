package domainmanager

import (
	mapset "github.com/deckarep/golang-set"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"net"
	"strings"
)

const HostnameLabel = "kubernetes.io/hostname"


// Cache entry representing one node and its state
type NodeEntry struct {
	ipListv4       []net.IP
	ipListv6       []net.IP
	name           string
	hostname       string
	managedDomains mapset.Set
	logger *logrus.Entry
}

type NodeInformer struct {
	nodeCache      map[string]*NodeEntry
	domainManagers []*DomainManager
	logger *logrus.Entry
}

func (n *NodeEntry) EnsureDomains(delete bool) {
	for _, managedDomain := range n.managedDomains.ToSlice() {
		domainEntry := managedDomain.(*DomainEntry)
		err := managedDomain.(*DomainEntry).responsibleDomainHandler.EnsureDomain(n, domainEntry, delete)

		if err != nil {
			n.logger.WithField("domain", domainEntry.domain).Warnf("Could not ensure DNS records")
		}
	}
}

func (inf *NodeInformer)GetDomainEntryList(annotations map[string]string) mapset.Set {

	domainEntries := mapset.NewSet()


	for key, value := range annotations {
		if !strings.HasSuffix(key, "/domainmanager") {
			continue
		}
		domain := strings.Split(key, "/")[0]
		domainParts := strings.Split(domain, ".")
		if len(domainParts) < 2 {
			inf.logger.Warnf("Invalid dns record: %s, ignoring", domain)
			continue
		}

		var responsibleDomainManager *DomainManager

		for _, domainManager := range inf.domainManagers {
			isResponsible := domainManager.domainHandler.CheckIfResponsible(domainParts)

			if isResponsible {
				responsibleDomainManager = domainManager
				break
			}
		}

		// If we do not have any domainManager, which seems to be responsible
		// for this domain, we do not add this to our managed domains.
		if responsibleDomainManager == nil {
			continue
		}

		if value == "wildcard" {
			wildcardDomain := "*."+domain
			wildcardDomainParts := strings.Split(wildcardDomain, ".")

			wildcardDomainEntry := DomainEntry{
				domain:                   wildcardDomain,
				domainParts:              wildcardDomainParts,
				isPresent:                true,
				responsibleDomainHandler: responsibleDomainManager,
			}

			domainEntries.Add(
				&wildcardDomainEntry,
			)
		}

		domainEntry := DomainEntry{
			domain:                   domain,
			domainParts:              domainParts,
			isPresent:                value == "true" || value == "present" || value == "wildcard",
			responsibleDomainHandler: responsibleDomainManager,
		}

		domainEntries.Add(
			&domainEntry,
		)
	}
	return domainEntries
}


func (inf *NodeInformer) OnAdd(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		inf.logger.Warn("Invalid onAdd event, expected corev1.Node")
		return
	}
	if _, ok := inf.nodeCache[node.Name]; ok {
		inf.logger.WithField("node", node.Name).Warn("Node is already present in cache, ignoring")
		return
	}
	hostname := node.Labels[HostnameLabel]
	ipList, err := net.LookupIP(hostname)
	if err != nil {
		inf.logger.WithFields(logrus.Fields{"hostname": hostname, "node": node.Name}).Warn("Failed to get DNS records for node, ignoring")
		return
	}
	nodeLogger := inf.logger.WithField("node", node.Name)
	cacheEntry := &NodeEntry{
		name:           node.Name,
		hostname:       hostname,
		ipListv4:       []net.IP{},
		ipListv6:       []net.IP{},
		managedDomains: inf.GetDomainEntryList(node.Annotations),
		logger: nodeLogger,
	}

	for _, addr := range ipList {
		isIPv4 := addr.To4() != nil

		if isIPv4 {
			cacheEntry.ipListv4 = append(cacheEntry.ipListv4, addr)
		} else {
			cacheEntry.ipListv6 = append(cacheEntry.ipListv6, addr)
		}
	}

	nodeLogger.WithFields(logrus.Fields{"v4": len(cacheEntry.ipListv4), "v6": len(cacheEntry.ipListv6)}).Info("Found endpoints")
	cacheEntry.EnsureDomains(false)
	inf.nodeCache[node.Name] = cacheEntry
}
func (inf *NodeInformer) OnDelete(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		inf.logger.Warn("Invalid onDelete event, expected corev1.Node")
		return
	}
	cacheEntry, ok := inf.nodeCache[node.Name]
	inf.logger.WithField("node", node.Name).Info("Deleting Node")

	if !ok {
		inf.logger.WithField("node", node.Name).Warn("Failed to delete node, it does not exist")
		return
	}
	cacheEntry.EnsureDomains(true)
	delete(inf.nodeCache, node.Name)
}
func (inf *NodeInformer) OnUpdate(oldObj interface{}, newObj interface{}) {
	newNode, okNew := newObj.(*corev1.Node)
	if !okNew {
		inf.logger.Warn("Invalid onUpdate event, expected corev1.Node")
		return
	}
	node, ok := inf.nodeCache[newNode.Name]
	if !ok {
		inf.logger.WithField("node", newNode.Name).Warn("Invalid onUpdate event, expected corev1.Node")
		inf.OnAdd(newNode)
		return
	}

	managedDomains := inf.GetDomainEntryList(newNode.Annotations)
	if managedDomains.Equal(node.managedDomains) {
		return // node zones are the same
	}
	inf.logger.WithField("node", node.name).Info("Updated Node")
	node.managedDomains = managedDomains
	node.EnsureDomains(false)
}

func CreateNodeHandler(domainHandler []*DomainManager, logger *logrus.Entry) *NodeInformer {
	myHandler := &NodeInformer{
		nodeCache:      map[string]*NodeEntry{},
		domainManagers: domainHandler,
		logger: logger,
	}

	return myHandler
}
