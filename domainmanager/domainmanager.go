package domainmanager

import (
	"github.com/sirupsen/logrus"
	"net"
)

// Single entry
type DomainEntry struct {
	domain    string
	domainParts    []string
}

type DNSRecord struct {
	ID string
	recordType string
	recordEntry string
}

type DomainHandler interface {
	EnsureSingleRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord)
	DeleteSingleRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord)
	EnsureGroupedRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr []net.IP, recType string, records []*DNSRecord)
	DeleteGroupedRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr []net.IP, recType string, records []*DNSRecord)
	GetExistingDNSRecordsForDomain(domainEntry *DomainEntry) ([]*DNSRecord, error)
	CheckIfResponsible(domainParts []string) bool
	GetName() string
	GetAPIType() string
}

type DomainManager struct {
	domainHandler DomainHandler
	logger *logrus.Entry
}

func (dM *DomainManager) EnsureDomain(recordToCreate []string, recType string, domainEntry *DomainEntry, existingRecords []string) error {

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

	if dM.domainHandler.GetAPIType() == "grouped" {
		groupedIPv4List := make([]net.IP, 0)
		groupedIPv6List := make([]net.IP, 0)

		for _, nodeListEntry := range nodeList {
			groupedIPv4List = append(groupedIPv4List, nodeListEntry.ipListv4...)
			groupedIPv6List = append(groupedIPv6List, nodeListEntry.ipListv6...)
		}

		// Deleting last node of domain
		if delete && len(nodeList) == 1 && nodeList[0].name == nodeEntry.name {
			dM.domainHandler.DeleteGroupedRecord(nodeEntry, domainEntry, nodeEntry.ipListv4, "A", existingDNSRecords)
			dM.domainHandler.DeleteGroupedRecord(nodeEntry, domainEntry, nodeEntry.ipListv6, "AAAA", existingDNSRecords)
		} else {
			// Updating existing record
			dM.domainHandler.EnsureGroupedRecord(nodeEntry, domainEntry, groupedIPv4List, "A", existingDNSRecords)
			dM.domainHandler.EnsureGroupedRecord(nodeEntry, domainEntry, groupedIPv6List, "AAAA", existingDNSRecords)
		}
	} else {
		if domainEntry.isPresent && !delete {
			for _, addr := range nodeEntry.ipListv4 {
				dM.domainHandler.EnsureSingleRecord(nodeEntry, domainEntry, addr, "A", existingDNSRecords)
			}
			for _, addr := range nodeEntry.ipListv6 {
				dM.domainHandler.EnsureSingleRecord(nodeEntry, domainEntry, addr, "AAAA", existingDNSRecords)
			}
		} else {
			for _, addr := range nodeEntry.ipListv4 {
				dM.domainHandler.DeleteSingleRecord(nodeEntry, domainEntry, addr, "A", existingDNSRecords)
			}
			for _, addr := range nodeEntry.ipListv6 {
				dM.domainHandler.DeleteSingleRecord(nodeEntry, domainEntry, addr, "AAAA", existingDNSRecords)
			}
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