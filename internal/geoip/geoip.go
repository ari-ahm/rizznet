package geoip

import (
	"fmt"
	"net"
	"sync"

	"github.com/oschwald/geoip2-golang"
	"rizznet/internal/logger"
)

var (
	asnReader     *geoip2.Reader
	countryReader *geoip2.Reader
	once          sync.Once
	initErr       error
)

// Init loads the MMDB files from specific paths
func Init(asnPath, countryPath string) error {
	once.Do(func() {
		// 1. Load ASN DB
		if asnPath != "" {
			var err error
			asnReader, err = geoip2.Open(asnPath)
			if err != nil {
				initErr = fmt.Errorf("failed to open ASN DB at %s: %w", asnPath, err)
				return // Stop if critical DB fails
			}
		}

		// 2. Load Country DB
		if countryPath != "" {
			var err error
			countryReader, err = geoip2.Open(countryPath)
			if err != nil {
				// We log warning instead of failing, allowing partial functionality
				logger.Log.Warnf("Failed to open Country DB at %s: %v. Country data will be missing.", countryPath, err)
			}
		}
	})
	return initErr
}

type GeoResult struct {
	ISP     string
	Country string
}

func Lookup(ipStr string) (*GeoResult, error) {
	if asnReader == nil {
		return nil, fmt.Errorf("geoip database not initialized")
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip: %s", ipStr)
	}

	res := &GeoResult{ISP: "Unknown", Country: "XX"}

	// ASN Lookup
	if asn, err := asnReader.ASN(ip); err == nil {
		res.ISP = asn.AutonomousSystemOrganization
	}

	// Country Lookup
	if countryReader != nil {
		if c, err := countryReader.Country(ip); err == nil {
			res.Country = c.Country.IsoCode
		}
	}

	return res, nil
}

func Close() {
	if asnReader != nil {
		asnReader.Close()
	}
	if countryReader != nil {
		countryReader.Close()
	}
}
