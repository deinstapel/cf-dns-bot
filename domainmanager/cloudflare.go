package domainmanager

import (
	"fmt"
	"github.com/cloudflare/cloudflare-go"
	"net"
	"os"
)

type CloudflareDomainHandler struct {
	zoneCache map[string]string
	cfApi *cloudflare.API
}


func (cDH *CloudflareDomainHandler) getZoneIdFromZone(zone string) (string, error){
	if z, ok := cDH.zoneCache[zone]; ok {
		return z, nil
	}
	z, err := cDH.cfApi.ZoneIDByName(zone)
	if err == nil {
		fmt.Fprintf(os.Stderr, "[global] resolved zone '%s' to ID '%s'\n", zone, z)
		cDH.zoneCache[zone] = z
	}
	return z, err
}

// Retrieve a zone id from the cloudflare API or from the cache. Cache is preferred
func (cDH *CloudflareDomainHandler) GetZoneIdFromDomain(domainEntry *DomainEntry) (string, error) {
	domainParts := domainEntry.domainParts
	zone := fmt.Sprintf("%s.%s", domainParts[len(domainParts)-2], domainParts[len(domainParts)-1])
	return cDH.getZoneIdFromZone(zone)
}

func (cDH *CloudflareDomainHandler) EnsureSingleRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord) {
	addrString := addr.String()
	for _, rec := range records {
		if rec.recordType == recType && rec.recordEntry == addrString {
			// We already have the zone in our dns records, no action neccessary
			return
		}
	}

	zoneId, err := cDH.GetZoneIdFromDomain(domainEntry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] Failed to get zone for DNS record '%s': %v\n", nodeEntry.name, domainEntry.domain, err)
	}

	_, err = cDH.cfApi.CreateDNSRecord(zoneId, cloudflare.DNSRecord{
		Type:    recType,
		Name:    nodeEntry.name,
		Content: addr.String(),
		Proxied: false,
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] Failed to create DNS record '%s': %v\n", nodeEntry.name, domainEntry.domain, err)
	} else {
		fmt.Fprintf(os.Stderr, "[%s] DNS %s -> %s\n", nodeEntry.name, domainEntry.domain, addrString)
	}
}

func (cDH *CloudflareDomainHandler) DeleteSingleRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord) {
	addrString := addr.String()
	zoneId, err := cDH.GetZoneIdFromDomain(domainEntry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] Failed to get zone for DNS record '%s': %v\n", nodeEntry.name, domainEntry.domain, err)
	}
	for _, rec := range records {
		if rec.recordType == recType && rec.recordEntry == addrString {
			if err := cDH.cfApi.DeleteDNSRecord(zoneId, rec.ID); err != nil {
				fmt.Fprintf(os.Stderr, "[%s] Failed to delete DNS record '%s': %v\n", nodeEntry.name, domainEntry.domain, err)
			} else {
				fmt.Fprintf(os.Stderr, "[%s] Deleted DNS record %s @ %s\n", nodeEntry.name, domainEntry.domain, addrString)
			}
			return
		}
	}
}

func (cDH *CloudflareDomainHandler) GetExistingDNSRecordsForDomain(domainEntry *DomainEntry) ([]*DNSRecord, error) {
	zoneId, err := cDH.GetZoneIdFromDomain(domainEntry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get zone for DNS record '%s': %v\n", domainEntry.domain, err)
		return nil, err
	}

	// FIXME: What does Proxied:false does?
	records, err := cDH.cfApi.DNSRecords(zoneId, cloudflare.DNSRecord{Proxied: false})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to retrieve DNS records for zone '%s': %v\n", zoneId, err)
		return nil, err
	}

	dnsRecords := make([]*DNSRecord, 0)
	for _, cloudflareRecord := range records {

		dnsRecord := DNSRecord{
			ID:    cloudflareRecord.ID,
			recordType:  cloudflareRecord.Type,
			recordEntry: cloudflareRecord.Content,
		}
		dnsRecords = append(dnsRecords, &dnsRecord)
	}

	return dnsRecords, nil
}



func (cDH *CloudflareDomainHandler) CheckIfResponsible(domainParts []string) bool {

	// We try to get the zoneId for this domain. If this domain is not associated to our login account
	// or simply does not exist in Cloudflare, this request will fail.
	// Therefore, we know, that this domain is not associated to this domainManagers.
	zone := fmt.Sprintf("%s.%s", domainParts[len(domainParts)-2], domainParts[len(domainParts)-1])
	_, err := cDH.getZoneIdFromZone(zone)
	return err == nil

}

func (_ *CloudflareDomainHandler) GetName() string{
	return "cloudflare"
}

func (_ *CloudflareDomainHandler) EnsureGroupedRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr []net.IP, recType string, records []*DNSRecord) {
	panic("Not Implemented")
}
func (_ *CloudflareDomainHandler) DeleteGroupedRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr []net.IP, recType string, records []*DNSRecord) {
	panic("Not Implemented")
}

func (_ *CloudflareDomainHandler) GetAPIType() string{
	return "single"
}


func CreateCloudflareDomainHandler(cfApiMail string, cfApiKey string) (*CloudflareDomainHandler, error) {
	cfApi, err := cloudflare.New(cfApiKey, cfApiMail)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize CF API: %v", err)
		return nil, err
	}

	cloudflareDomainHander := CloudflareDomainHandler{
		zoneCache: map[string]string{},
		cfApi: cfApi,
	}

	return &cloudflareDomainHander, nil

}