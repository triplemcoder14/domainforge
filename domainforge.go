package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/oleksandr/bonjour"
	"github.com/triplemcoder14/domainforge/utils"
)

type Record struct {
	service string
	host    string
	server  *bonjour.Server
}

type DomainForge struct {
	records map[string]*Record
	mu      sync.Mutex
}

func NewDomainForge() *DomainForge {
	return &DomainForge{
		records: make(map[string]*Record),
	}
}

func (df *DomainForge) List() []string {
	df.mu.Lock()
	defer df.mu.Unlock()

	domains := make([]string, 0, len(df.records))
	for domain := range df.records {
		domains = append(domains, domain)
	}
	return domains
}

func (df *DomainForge) Add(domain string, port int) error {
	df.mu.Lock()
	defer df.mu.Unlock()

	config, err := utils.ReadConfig()
	if err != nil {
		return err
	}

	localIP, err := utils.GetLocalIP()
	if err != nil {
		log.Fatalln("Error getting local IP:", err.Error())
	}

	clean := strings.TrimSpace(domain)
	fullDomain := fmt.Sprintf("%s.local", clean)
	if _, exists := df.records[fullDomain]; exists {
		return fmt.Errorf("domain %s already registered", fullDomain)
	}
	fullHost := fmt.Sprintf("%s.", fullDomain)

	service := fmt.Sprintf("_%s._tcp", clean)
	// Register domainforge service
	s1, err := bonjour.RegisterProxy(
		"domainforge",
		service,
		"",
		80,
		fullHost,
		localIP,
		[]string{},
		nil)

	if err != nil {
		log.Fatalln("Error registering frontend service:", err.Error())
	}

	df.records[fullDomain] = &Record{
		service: service,
		host:    fullHost,
		server:  s1,
	}

	if err := addQulesServerBlock([]string{fullDomain}, port, config.QulesAdmin); err != nil {
		s1.Shutdown()
		delete(df.records, domain)
		return fmt.Errorf("failed to add Qules server block: %v", err)
	}
	return nil
}

func (df *DomainForge) Remove(domain string) error {
	df.mu.Lock()
	defer df.mu.Unlock()

	record, exists := df.records[domain]
	if !exists {
		return fmt.Errorf("domain %s not registered", domain)
	}

	record.server.Shutdown()
	delete(df.records, domain)
	log.Printf("Removed domain: %s", domain)
	return nil
}

func (df *DomainForge) Shutdown() {
	df.mu.Lock()
	defer df.mu.Unlock()

	for domain, rec := range df.records {
		rec.server.Shutdown()
		log.Printf("Shutting down domain: %s", domain)
	}
}

func (df *DomainForge) startBroadcast(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			df.broadcastAll()
		case <-ctx.Done():
			return
		}
	}
}

func (df *DomainForge) broadcastAll() {
	df.mu.Lock()
	defer df.mu.Unlock()

	localIP, err := utils.GetLocalIP()
	if err != nil {
		log.Fatalln("Error getting local IP:", err.Error())
	}

	for domain, info := range df.records {
		info.server.Shutdown()

		server, err := bonjour.RegisterProxy(
			"domainforge",
			info.service,
			"",
			80,
			info.host,
			localIP,
			[]string{},
			nil)

		if err != nil {
			log.Fatalln("Error registering frontend service:", err.Error())
		}

		if err != nil {
			log.Printf("Error re-registering service for %s: %v", domain, err)
			continue
		}

		info.server = server
	}
}
