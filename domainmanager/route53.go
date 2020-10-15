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
	"strings"
)

type Route53DomainHandler struct {
	route53Api *route53.Route53
	hostedZoneCache map[string]*route53.HostedZone
	logger *logrus.Entry
}

func unescapeRoute53URL(s string) string {

	retS := s

	if strings.HasSuffix(s, ".") {
		retS = strings.TrimSuffix(s, ".")
	}

	return strings.ReplaceAll(retS, "\\052", "*")


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

func (domainHandler *Route53DomainHandler) EnsureSingleRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord) {
	panic("Not Implemeted")
}
func (domainHandler *Route53DomainHandler) DeleteSingleRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr net.IP, recType string, records []*DNSRecord) {
	panic("Not Implemented")
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

		escapedDomain := unescapeRoute53URL(*resourceRecord.Name)
		isSameDomain := escapedDomain == domainEntry.domain

		// We only need A and AAAA records. We do not manage other records.
		if !isSameDomain || (*resourceRecord.Type != "A" && *resourceRecord.Type != "AAAA") {
			continue
		}

		for _, resourceRecordEntry := range resourceRecord.ResourceRecords {

			dnsRecords = append(dnsRecords, &DNSRecord{
				ID:          uuid.New().String(), // Technically, we do not need this
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


func (domainHandler *Route53DomainHandler) EnsureGroupedRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr []net.IP, recType string, records []*DNSRecord) {
	logger := domainHandler.logger.WithField("node", nodeEntry.name).WithField("domain", domainEntry.domain).WithField("type", recType)
	hostedZone, err := domainHandler.GetHostedZoneForDomain(domainEntry.domainParts);

	// FIXME: Evaluate existing records to save some API calls.

	logger.Info("EnsureGroupedRecord called")

	if err != nil {
		logger.Warn("Could not get HostedZone")
		return
	}

	resourceRecords := make([]*route53.ResourceRecord, 0)

	for _, ipAddr := range addr {
		resourceRecords = append(
			resourceRecords,
			&route53.ResourceRecord{
				Value: aws.String(ipAddr.String()),
			},
		)
	}

	batchRequest := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String("UPSERT"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						ResourceRecords: resourceRecords,
						Name: aws.String(domainEntry.domain),
						Type: aws.String(recType),
						TTL: aws.Int64(60),
					},
				},
			},
			Comment: aws.String("Managed by dns-bot"),
		},
		HostedZoneId: hostedZone.Id,
	}

	_, err = domainHandler.route53Api.ChangeResourceRecordSets(batchRequest)

	if err != nil {
		logger.WithError(err).Warn("Failed Routes Update")
	} else {
		logger.Info("Updated Routes")
	}

}
func (domainHandler *Route53DomainHandler) DeleteGroupedRecord(nodeEntry *NodeEntry, domainEntry *DomainEntry, addr []net.IP, recType string, records []*DNSRecord) {
	logger := domainHandler.logger.WithField("node", nodeEntry.name).WithField("domain", domainEntry.domain).WithField("type", recType)
	hostedZone, err := domainHandler.GetHostedZoneForDomain(domainEntry.domainParts);

	// FIXME: Evaluate existing records to save some API calls.

	logger.Info("DeleteGroupedRecord called")

	if err != nil {
		logger.Warn("Could not get HostedZone")
		return
	}

	resourceRecords := make([]*route53.ResourceRecord, 0)

	for _, ipAddr := range addr {
		resourceRecords = append(
			resourceRecords,
			&route53.ResourceRecord{
				Value: aws.String(ipAddr.String()),
			},
		)
	}

	batchRequest := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String("DELETE"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						ResourceRecords: resourceRecords,
						Name: aws.String(domainEntry.domain),
						Type: aws.String(recType),
						TTL: aws.Int64(60),
					},
				},
			},
		},
		HostedZoneId: hostedZone.Id,
	}

	_, err = domainHandler.route53Api.ChangeResourceRecordSets(batchRequest)

	if err != nil {
		logger.WithError(err).Warn("Failed Route Deletion")
	} else {
		logger.Info("Deleted Routes")
	}
}

func (_ *Route53DomainHandler) GetAPIType() string{
	return "grouped"
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