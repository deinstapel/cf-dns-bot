package domainmanager

import (
	"errors"
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
	logger *logrus.Entry
}

type NodeInformer struct {
	nodeManager      *NodeManager
	logger *logrus.Entry
}

type NodeManager struct {
	// name -> nodeEntry
	nodeCache      map[string]*NodeEntry
	// Available Domain Managers
	availableDomainManagers []*DomainManager

	domains         map[string]*DomainEntry
	domainsManagers map[string]*DomainManager

	nodeDomains map[string][]*DomainEntry

	logger *logrus.Entry
}

func (nodeManager *NodeManager) AddNode(node *corev1.Node) error {

	if _, ok := nodeManager.nodeCache[node.Name]; ok {
		return errors.New(node.Name + " already exists in NodeManager")
	}
	hostname := node.Labels[HostnameLabel]
	ipList, err := net.LookupIP(hostname)
	if err != nil {
		return errors.New("Could not get IPs for " + node.Name)
	}
	nodeLogger := logrus.WithField("node", node.Name)
	nodeEntry := &NodeEntry{
		name:           node.Name,
		hostname:       hostname,
		ipListv4:       []net.IP{},
		ipListv6:       []net.IP{},
		logger: nodeLogger,
	}

	for _, addr := range ipList {
		isIPv4 := addr.To4() != nil

		if isIPv4 {
			nodeEntry.ipListv4 = append(nodeEntry.ipListv4, addr)
		} else {
			nodeEntry.ipListv6 = append(nodeEntry.ipListv6, addr)
		}
	}

	nodeManager.nodeCache[node.Name] = nodeEntry
	nodeManager.UpdateAnnotations(nodeEntry, node.Annotations)
	return nil
}

func (nodeManager *NodeManager) DeleteNode(node *corev1.Node) error {
	nodeManager.logger.WithField("node", node.Name).Info("Deleting Node")
	nodeEntry, ok := nodeManager.nodeCache[node.Name]

	if !ok {
		return errors.New(node.Name + " does not exist")
	}
	delete(nodeManager.nodeCache, node.Name)
	nodeManager.UpdateAnnotations(nodeEntry, map[string]string{})

	return nil
}
func (nodeManager *NodeManager) UpdateNode(node *corev1.Node) error {
	nodeManager.logger.WithField("node", node.Name).Info("Update Node")
	nodeEntry, ok := nodeManager.nodeCache[node.Name]

	if !ok {
		return nodeManager.AddNode(node)
	}

	nodeManager.UpdateAnnotations(nodeEntry, node.Annotations)
	return nil
}

func getDomainsFromAnnotations(annotations map[string]string) []string {

	retDomains := make([]string, 0)

	for key, value := range annotations {
		var domain string
		if strings.HasSuffix(key, "/domainmanager/wildcard") {
			domain = "*." + strings.Split(key, "/")[0]

		} else if strings.HasSuffix(key, "/domainmanager") {
			domain = strings.Split(key, "/")[0]
		} else {
			continue
		}

		if value != "true" {
			continue
		}

		retDomains = append(retDomains, domain)
	}
	return retDomains
}


func (nodeManager *NodeManager) GetDomainDetails(domain string) (domainEntry *DomainEntry, domainHandler *DomainManager, hasDomainManager bool) {
	existingDomain, existingDomainOk := nodeManager.domains[domain]
	if !existingDomainOk {
		existingDomain = &DomainEntry{
			domain:      domain,
			domainParts: strings.Split(domain, "."),
		}
		nodeManager.domains[domain] = existingDomain
	}

	existingDomainManager, existingDomainManagerOk := nodeManager.domainsManagers[domain]
	if !existingDomainManagerOk {

		var responsibleDomainManager *DomainManager
		for _, availableDomainManager := range nodeManager.availableDomainManagers {
			if availableDomainManager.domainHandler.CheckIfResponsible(existingDomain.domainParts) {
				responsibleDomainManager = availableDomainManager
			}
		}

		// This can be nil. If this is nil, we do not have a responsible domain manager for this
		// domain. Therefore, we do not do any actions on this domain at all.
		existingDomainManager = responsibleDomainManager
		nodeManager.domainsManagers[domain] = responsibleDomainManager
	}

	return existingDomain, existingDomainManager, existingDomainManager != nil
}

func (nodeManager *NodeManager) UpdateAnnotations(nodeEntry *NodeEntry, annotations map[string]string) {

	domainsToAdd := make([]string, 0)
	domainsToRemove := make([]string, 0)

	newDomains := getDomainsFromAnnotations(annotations)

	existingDomains, existingDomainsOk := nodeManager.nodeDomains[nodeEntry.name]
	if !existingDomainsOk {
		existingDomains = []*DomainEntry{}
	}

	for _, newDomain := range newDomains {
		domainExists := false
		for _, existingDomain := range existingDomains {
			if newDomain == existingDomain.domain {
				domainExists = true
			}
		}
		if !domainExists {
			domainsToAdd = append(domainsToAdd, newDomain)
		}
	}

	for _, existingDomain := range existingDomains {
		domainStillExists := false
		for _, newDomain := range newDomains {
			if existingDomain.domain == newDomain {
				domainStillExists = true
			}
		}

		if !domainStillExists {
			domainsToRemove = append(domainsToRemove, existingDomain.domain)
		}
	}

	for _, domainToAdd := range domainsToAdd {
		domain, domainManager, isManaged := nodeManager.GetDomainDetails(domainToAdd)

		// We ignore domains, which we do not manage.
		if !isManaged {
			continue
		}

		dnsRecords, err := domainManager.domainHandler.GetExistingDNSRecordsForDomain(domain)

		if err != nil {
			nodeManager.logger.WithError(err).Warnf("Failed to get existing DNS records")
		}

		dnsRecordsToCreate := make([]string,0)
		for _, IPv4 := range nodeEntry.ipListv4 {
			needsCreation := true
			for _, dnsRecord := range dnsRecords {
				if dnsRecord.recordType == "A" && IPv4.String() == dnsRecord.recordEntry {
					needsCreation = false
				}
			}
			if needsCreation {
				dnsRecordsToCreate = append(dnsRecordsToCreate, IPv4.String())
			}
		}
		for _, IPv6 := range nodeEntry.ipListv4 {
			needsCreation := true
			for _, dnsRecord := range dnsRecords {
				if dnsRecord.recordType == "AAAA" && IPv6.String() == dnsRecord.recordEntry {
					needsCreation = false
				}
			}
			if needsCreation {
				dnsRecordsToCreate = append(dnsRecordsToCreate, IPv6.String())
			}
		}

		for _, dnsRecordToCreate := range dnsRecordsToCreate {
			domainManager.Ensure
		}
	}

}

func (nodeInformer *NodeInformer)GetDomainEntryList(annotations map[string]string) mapset.Set {

	domainEntries := mapset.NewSet()


	for key, value := range annotations {
		if !strings.HasSuffix(key, "/domainmanager") {
			continue
		}
		domain := strings.Split(key, "/")[0]
		domainParts := strings.Split(domain, ".")
		if len(domainParts) < 2 {
			nodeInformer.logger.Warnf("Invalid dns record: %s, ignoring", domain)
			continue
		}

		var responsibleDomainManager *DomainManager

		for _, domainManager := range nodeInformer.domainManagers {
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

		domainEntry := DomainEntry{
			domain:                   domain,
			domainParts:              domainParts,
		}

		domainEntries.Add(
			&domainEntry,
		)
	}
	return domainEntries
}


func (nodeInformer *NodeInformer) OnAdd(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		nodeInformer.logger.Warn("Invalid onAdd event, expected corev1.Node")
		return
	}

	err := nodeInformer.nodeManager.AddNode(node)
	if err != nil {
		nodeInformer.logger.WithError(err).Warn("Could not add " + node.Name)
	}
}
func (nodeInformer *NodeInformer) OnDelete(obj interface{}) {
	deleteNode, ok := obj.(*corev1.Node)
	if !ok {
		nodeInformer.logger.Warn("Invalid onDelete event, expected corev1.Node")
		return
	}

	err := nodeInformer.nodeManager.DeleteNode(deleteNode)
	if err != nil {
		nodeInformer.logger.WithError(err).Warn("Could not delete " + deleteNode.Name)
	}
}
func (nodeInformer *NodeInformer) OnUpdate(oldObj interface{}, newObj interface{}) {
	updatedNode, okNew := newObj.(*corev1.Node)
	if !okNew {
		nodeInformer.logger.Warn("Invalid onUpdate event, expected corev1.Node")
		return
	}
	err := nodeInformer.nodeManager.UpdateNode(updatedNode)
	if err != nil {
		nodeInformer.logger.WithError(err).Warn("Could not node " + updatedNode.Name)
	}
}

func CreateNodeHandler(domainManagers []*DomainManager, logger *logrus.Entry) *NodeInformer {
	myHandler := &NodeInformer{
		nodeManager: &NodeManager{
			nodeCache:               map[string]*NodeEntry{},
			availableDomainManagers: domainManagers,
			domains:                 map[string]*DomainEntry{},
			domainsManagers:         map[string]*DomainManager{},
			nodeDomains:             map[string][]*DomainEntry{},
			logger:                  logger.WithField("app", "node-manager"),
		},
		logger: logger,
	}

	return myHandler
}
