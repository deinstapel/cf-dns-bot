package domainmanager

import (
	"github.com/sirupsen/logrus"
	"net"
)

// Single entry
type DomainEntry struct {
	domain    string
	domainParts    []string
	isPresent bool
	responsibleDomainHandler *DomainManager
}

type DNSRecord struct {
	ID string
	recordType string
	recordEntry string
}

type DomainHandler interface {
	EnsureRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord)
	EnsureDeleted(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord)
	GetExistingDNSRecordsForDomain(domainEntry *DomainEntry) ([]*DNSRecord, error)
	CheckIfResponsible(domainParts []string) bool
	GetName() string
}

type DomainManager struct {
	domainHandler DomainHandler
	logger *logrus.Entry
}

func (dM *DomainManager) EnsureDomain(nodeEntry *NodeEntry, domainEntry *DomainEntry, delete bool) error {

	nodeLogger := dM.logger.WithField("node", nodeEntry.name).WithField("domain", domainEntry.domain)
	nodeLogger.Infof("Checking domain")

	// Get all DNS entries for this domain.
	// There will also be entries for other nodes, which we are not handling right now.
	// We pass existingDNSRecords into our Ensure-functions to be able to save some API calls.
	existingDNSRecords, err := dM.domainHandler.GetExistingDNSRecordsForDomain(domainEntry)
	if err != nil {
		nodeLogger.WithError(err).WithField("domain", domainEntry.domain).Warnf( "Failed to get existing DNS records")
		return err
	}

	if domainEntry.isPresent && !delete {
		for _, addr := range nodeEntry.ipListv4 {
			dM.domainHandler.EnsureRecord(nodeEntry, domainEntry, addr, "A", existingDNSRecords)
		}
		for _, addr := range nodeEntry.ipListv6 {
			dM.domainHandler.EnsureRecord(nodeEntry, domainEntry, addr, "AAAA", existingDNSRecords)
		}
	} else {
		for _, addr := range nodeEntry.ipListv4 {
			dM.domainHandler.EnsureDeleted(nodeEntry, domainEntry, addr, "A", existingDNSRecords)
		}
		for _, addr := range nodeEntry.ipListv6 {
			dM.domainHandler.EnsureDeleted(nodeEntry, domainEntry, addr, "AAAA", existingDNSRecords)
		}
	}

	return nil
}

func CreateDomainMananger(domainHandler DomainHandler, logger *logrus.Entry) *DomainManager {
	domainManager := DomainManager{
		domainHandler: domainHandler,
		logger: logger.WithField("domain-handler", domainHandler.GetName()),
	}

	return &domainManager
}