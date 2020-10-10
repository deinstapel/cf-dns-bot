package domainmanager

import (
	"fmt"
	mapset "github.com/deckarep/golang-set"
	"github.com/google/uuid"
	"net"
	"os"
)

type DummyDomainHandler struct {
	dnsRecords map[string]mapset.Set
}

func (dHandler *DummyDomainHandler) GetDNSRecordsForDomain(domain string) mapset.Set {
	if dHandler.dnsRecords[domain] == nil {
		dHandler.dnsRecords[domain] = mapset.NewSet()
	}

	return dHandler.dnsRecords[domain]
}

func (dHandler *DummyDomainHandler) EnsureRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, _ []*DNSRecord){
	fmt.Fprintf(os.Stderr, "[%s] Ensure DNS Record addr=%v, type=%v, domain=%v\n", nodeEntry.name, addr.String(), recType, domainEntry.domain)

	domainDNSRecords := dHandler.GetDNSRecordsForDomain(domainEntry.domain)
	for _, recRaw := range domainDNSRecords.ToSlice() {
		rec := recRaw.(*DNSRecord)
		if rec.recordType == recType && rec.recordEntry == addr.String() {
			// We already have the zone in our dns domainDNSRecords, no action neccessary
			fmt.Fprintf(os.Stderr, "[%s] Found existing DNS Record type=%v, addr=%v\n", nodeEntry.name, rec.recordEntry, addr.String())
			return
		}
	}

	recordUUID, _ := uuid.NewUUID()
	dnsRecord := DNSRecord{
		ID:    recordUUID.String(),
		recordType:  recType,
		recordEntry: addr.String(),
	}
	dHandler.dnsRecords[domainEntry.domain].Add(&dnsRecord)
	fmt.Fprintf(os.Stderr, "[%s] Created DNS Record %s -> %s\n", nodeEntry.name, domainEntry.domain, addr.String())

}
func (dHandler *DummyDomainHandler) EnsureDeleted(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, _ []*DNSRecord){
	domainDNSRecords := dHandler.GetDNSRecordsForDomain(domainEntry.domain)
	for _, recRaw := range domainDNSRecords.ToSlice() {
		rec := recRaw.(*DNSRecord)
		if rec.recordType == recType && rec.recordEntry == addr.String() {
			domainDNSRecords.Remove(recRaw)
			fmt.Fprintf(os.Stderr, "[%s] Deleted DNS Record '%s'\n", nodeEntry.name, domainEntry.domain)
		}
	}
}

func (dHandler *DummyDomainHandler) GetExistingDNSRecordsForDomain(domainEntry *DomainEntry) ([]*DNSRecord, error) {
	dnsRecords := make([]*DNSRecord, 0)
	dnsRecordsSet := dHandler.GetDNSRecordsForDomain(domainEntry.domain)

	dnsRecordsSet.Each(func (e interface{}) bool{
		dnsRecords = append(dnsRecords, e.(*DNSRecord))
		return false
	})

	return dnsRecords, nil
}

func (dHandler *DummyDomainHandler) CheckIfResponsible(domainParts []string) bool{
	fmt.Fprintf(os.Stderr, "CheckIfResponsible for %v\n", domainParts)
	return true
}
func (dHandler *DummyDomainHandler) GetName() string{
	return "dummy"
}

func CreateDummyHandler() *DummyDomainHandler {
	return &DummyDomainHandler{
		dnsRecords: map[string]mapset.Set{},
	}
}