package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cloudflare/cloudflare-go"
	mapset "github.com/deckarep/golang-set"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const ManagedLabel = "domainmanager.deinstapel.de"
const HostnameLabel = "kubernetes.io/hostname"

var cfApi *cloudflare.API
var cfZoneCache map[string]string

// Retrieve a zone id from the cloudflare API or from the cache. Cache is preferred
func getZoneId(zone string) (string, error) {
	if z, ok := cfZoneCache[zone]; ok {
		return z, nil
	}
	z, err := cfApi.ZoneIDByName(zone)
	if err == nil {
		fmt.Fprintf(os.Stderr, "[global] resolved zone '%s' to ID '%s'\n", zone, z)
		cfZoneCache[zone] = z
	}
	return z, err
}

// Single entry
type nsEntry struct {
	zone    string
	name    string
	present bool
}

// Build a set of domain names to be managed by this tool
func extractDomainSet(annotations map[string]string) mapset.Set {
	domains := mapset.NewSet()
	for key, value := range annotations {
		if !strings.HasSuffix(key, "/domainmanager") {
			continue
		}
		domain := strings.Split(key, "/")[0]
		domainParts := strings.Split(domain, ".")
		if len(domainParts) < 2 {
			fmt.Fprintf(os.Stderr, "[global] Invalid dns record: %s, ignoring", domain)
			continue
		}
		zone := fmt.Sprintf("%s.%s", domainParts[len(domainParts)-2], domainParts[len(domainParts)-1])
		zoneId, err := getZoneId(zone)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[global] Failed to fetch zone id for zone '%s', make sure it's in your account.\n", zone)
			fmt.Fprintf(os.Stderr, "[global] Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "[global] DNS entry '%s' will NOT be managed.\n", domain)
		}
		domains.Add(nsEntry{
			zone:    zoneId,
			name:    domain,
			present: value == "true" || value == "present",
		})
	}
	return domains
}

// Cache entry representing one node and its state
type nodeCacheEntry struct {
	ipListv4       []net.IP
	ipListv6       []net.IP
	name           string
	hostname       string
	managedDomains mapset.Set
}

func (ce *nodeCacheEntry) ensureRecord(entry nsEntry, addr net.IP, recType string, records []cloudflare.DNSRecord) {
	addrString := addr.String()
	for _, rec := range records {
		if rec.Type == recType && rec.Content == addrString {
			// We already have the zone in our dns records, no action neccessary
			return
		}
	}

	_, err := cfApi.CreateDNSRecord(entry.zone, cloudflare.DNSRecord{
		Type:    recType,
		Name:    entry.name,
		Content: addr.String(),
		Proxied: false,
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] Failed to create DNS record '%s': %v\n", ce.name, entry.name, err)
	} else {
		fmt.Fprintf(os.Stderr, "[%s] DNS %s -> %s\n", ce.name, entry.name, addrString)
	}
}

func (ce *nodeCacheEntry) ensureDeleted(entry nsEntry, addr net.IP, recType string, records []cloudflare.DNSRecord) {
	addrString := addr.String()
	for _, rec := range records {
		if rec.Type == recType && rec.Content == addrString {
			if err := cfApi.DeleteDNSRecord(entry.zone, rec.ID); err != nil {
				fmt.Fprintf(os.Stderr, "[%s] Failed to delete DNS record '%s': %v\n", ce.name, entry.name, err)
			} else {
				fmt.Fprintf(os.Stderr, "[%s] Deleted DNS record %s @ %s\n", ce.name, entry.name, addrString)
			}
			return
		}
	}
}

// Ensure the dns records in cloudflare match the desired ones
// if delete is set to true, all records will be deleted, no matter of which records we previously had
func (ce *nodeCacheEntry) ensureDomains(delete bool) {
	ce.managedDomains.Each(func(entryObj interface{}) bool {
		entry := entryObj.(nsEntry)
		records, err := cfApi.DNSRecords(entry.zone, cloudflare.DNSRecord{Name: entry.name, Proxied: false})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] Failed to retrieve DNS records for zone '%s': %v\n", ce.name, entry.zone, err)
			return false
		}
		if entry.present && !delete {
			for _, addr := range ce.ipListv4 {
				ce.ensureRecord(entry, addr, "A", records)
			}
			for _, addr := range ce.ipListv6 {
				ce.ensureRecord(entry, addr, "AAAA", records)
			}
		} else {
			for _, addr := range ce.ipListv4 {
				ce.ensureDeleted(entry, addr, "A", records)
			}
			for _, addr := range ce.ipListv6 {
				ce.ensureDeleted(entry, addr, "AAAA", records)
			}
		}
		return false
	})
}

type nodeInformer struct {
	nodeCache map[string]*nodeCacheEntry
}

func (inf *nodeInformer) OnAdd(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		fmt.Fprintf(os.Stderr, "Expected node object in node add!\n")
		return
	}
	if _, ok := inf.nodeCache[node.Name]; ok {
		fmt.Fprintf(os.Stderr, "[%s] Node already present in cache!\n", node.Name)
		return
	}
	fmt.Printf("[%s] Creating cache entry\n", node.Name)
	hostname := node.Labels[HostnameLabel]
	ipList, err := net.LookupIP(hostname)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] DNS lookup failed (hostname %s)\n", node.Name, hostname)
		return
	}
	cacheEntry := &nodeCacheEntry{
		name:           node.Name,
		hostname:       hostname,
		ipListv4:       []net.IP{},
		ipListv6:       []net.IP{},
		managedDomains: extractDomainSet(node.Annotations),
	}
	for _, addr := range ipList {
		if len(addr) == 4 {
			cacheEntry.ipListv4 = append(cacheEntry.ipListv4, addr)
		} else if len(addr) == 16 {
			cacheEntry.ipListv6 = append(cacheEntry.ipListv6, addr)
		} else {
			fmt.Fprintf(os.Stderr, "Invalid ip address for node %s: %v, ignoring\n", node.Name, addr)
		}
	}
	fmt.Printf("[%s] Got %d v4 addrs, %d v6 addrs\n", node.Name, len(cacheEntry.ipListv4), len(cacheEntry.ipListv6))
	cacheEntry.ensureDomains(false)
	inf.nodeCache[node.Name] = cacheEntry
}
func (inf *nodeInformer) OnDelete(obj interface{}) {
	node, ok := obj.(*corev1.Node)
	if !ok {
		fmt.Fprintf(os.Stderr, "Failed to cast node in nodeDelete\n")
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] Node deleted\n", node.Name)
	ce, ok := inf.nodeCache[node.Name]
	if !ok {
		fmt.Fprintf(os.Stderr, "[%s] Node already gone, not deleting any entries")
		return
	}
	ce.ensureDomains(true)
	delete(inf.nodeCache, node.Name)
}
func (inf *nodeInformer) OnUpdate(oldObj interface{}, newObj interface{}) {
	newNode, okNew := newObj.(*corev1.Node)
	if !okNew {
		fmt.Fprintf(os.Stderr, "Failed to cast nodes in nodeUpdate\n")
		return
	}
	node, ok := inf.nodeCache[newNode.Name]
	if !ok {
		fmt.Fprintf(os.Stderr, "[%s] Node not in cache, assuming create")
		inf.OnAdd(newNode)
		return
	}
	zoneList := extractDomainSet(newNode.Annotations)
	if zoneList.Equal(node.managedDomains) {
		return // node zones are the same
	}
	fmt.Fprintf(os.Stderr, "[%s] Node updated\n", newNode.Name)
	node.managedDomains = zoneList
	node.ensureDomains(false)
}

func main() {
	cfZoneCache = map[string]string{}
	cfApiKey, keyOk := os.LookupEnv("CF_API_KEY")
	cfApiMail, mailOk := os.LookupEnv("CF_API_EMAIL")
	if !keyOk || !mailOk {
		fmt.Fprintf(os.Stderr, "Set CF_API_KEY and CF_API_EMAIL to use this program!\n")
		os.Exit(1)
	}

	var config *rest.Config
	var err error
	if kubeconfig, ok := os.LookupEnv("KUBECONFIG"); ok {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize kubeconfig: %v", err)
		os.Exit(1)
	}
	cfApi, err = cloudflare.New(cfApiKey, cfApiMail)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize CF API: %v", err)
		os.Exit(1)
	}
	clientSet := kubernetes.NewForConfigOrDie(config)

	signalChan := make(chan os.Signal)
	stopper, cancel := context.WithCancel(context.Background())
	go func() {
		<-signalChan
		cancel()
	}()
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	labelSelector := labels.Set(map[string]string{ManagedLabel: "yes"}).AsSelector()
	informer := cache.NewSharedIndexInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = labelSelector.String()
			return clientSet.CoreV1().Nodes().List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = labelSelector.String()
			return clientSet.CoreV1().Nodes().Watch(options)
		},
	}, &corev1.Node{}, 0, cache.Indexers{})
	myHandler := &nodeInformer{
		nodeCache: map[string]*nodeCacheEntry{},
	}
	informer.AddEventHandler(myHandler)
	informer.Run(stopper.Done())
}
