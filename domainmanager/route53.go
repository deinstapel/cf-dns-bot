package domainmanager

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"net"
	"os"
)

type Route53DomainHandler struct {
	route53Api *route53.Route53
	hostedZoneCache map[string]*route53.HostedZone
	logger *logrus.Entry
}

func (domainHandler *Route53DomainHandler) GetHostedZoneForDomain(domainParts []string) (*route53.HostedZone, error){
	dnsName := fmt.Sprintf("%s.%s.", domainParts[len(domainParts)-2], domainParts[len(domainParts)-1])
	if cachedHostedZone, ok := domainHandler.hostedZoneCache[dnsName]; ok {
		return cachedHostedZone, nil
	}

	hostedZones, err := domainHandler.route53Api.ListHostedZonesByName(&route53.ListHostedZonesByNameInput{DNSName: &dnsName})

	if err != nil {
		return nil, err
	}

	if len(hostedZones.HostedZones) < 1 {
		return nil, errors.New("HostedZones was empty")
	}

	hostedZone := hostedZones.HostedZones[0]
	if *hostedZone.Name != dnsName {
		return nil, errors.New("Invalid Reply from Route53")
	}
 	domainHandler.hostedZoneCache[dnsName] = hostedZone
	fmt.Fprintf(os.Stderr, "[Route53] Cached HostedZone '%v'\n", *hostedZone.Name)

	return hostedZone, nil
}

func (domainHandler *Route53DomainHandler) EnsureRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord) {

}
func (domainHandler *Route53DomainHandler) EnsureDeleted(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord) {

}
func (domainHandler *Route53DomainHandler) GetExistingDNSRecordsForDomain(domainEntry *DomainEntry) ([]*DNSRecord, error) {

	hostedZone, err := domainHandler.GetHostedZoneForDomain(domainEntry.domainParts)

	if err != nil {
		return nil, err
	}

	resourceRecords, _ := domainHandler.route53Api.ListResourceRecordSets(&route53.ListResourceRecordSetsInput{
		HostedZoneId: hostedZone.Id,
	})

	dnsRecords := make([]*DNSRecord, 0)


	for _, resourceRecord := range resourceRecords.ResourceRecordSets {

		// We only need A and AAAA records. We do not manage other records.
		if *resourceRecord.Type != "A" && *resourceRecord.Type != "AAAA" {
			continue
		}

		for _, resourceRecordEntry := range resourceRecord.ResourceRecords {

			dnsRecords = append(dnsRecords, &DNSRecord{
				ID:          uuid.New().String(),
				recordType:  *resourceRecord.Type,
				recordEntry: *resourceRecordEntry.Value,
			})
		}
	}
	
	return dnsRecords, nil
}
func (domainHandler *Route53DomainHandler) CheckIfResponsible(domainParts []string) bool {
	_, err := domainHandler.GetHostedZoneForDomain(domainParts)
	return err == nil
}

func (_ *Route53DomainHandler) GetName() string{
	return "route53"
}


func CreateRoute53RouteHandler(awsAccessKey string, awsSecretKey string) *Route53DomainHandler {

	awsCredentials := credentials.NewStaticCredentials(awsAccessKey, awsSecretKey, "")
	mySession := session.Must(session.NewSession())
	route53Api := route53.New(mySession, aws.NewConfig().WithCredentials(awsCredentials).WithRegion("eu-central-1"))

	route53DomainHandler := Route53DomainHandler{
		route53Api: route53Api,
		hostedZoneCache: map[string]*route53.HostedZone{},
		logger: logrus.WithField("domain-handler", "route53"),
	}

	return &route53DomainHandler
}