package geoip

import (
	_ "embed"
	"errors"
	"net"
	"strings"
	"sync"

	maxminddb "github.com/oschwald/maxminddb-golang"
)

//go:embed geoip.db
var db []byte

var (
	dbOnce = sync.OnceValues(func() (*maxminddb.Reader, error) {
		db, err := maxminddb.FromBytes(db)
		if err != nil {
			return nil, err
		}
		return db, nil
	})
)

type IPRecord struct {
	// IPInfo format (e.g. "US", "CN")
	CountryCode   string `maxminddb:"country_code"`
	ContinentCode string `maxminddb:"continent_code"`

	// MaxMind / GeoLite2 format
	Country struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	RegisteredCountry struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"registered_country"`
	RepresentedCountry struct {
		IsoCode string `maxminddb:"iso_code"`
	} `maxminddb:"represented_country"`
	Continent struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"continent"`
}

func Lookup(ip net.IP) (string, error) {
	if ip == nil {
		return "", errors.New("nil IP")
	}

	db, err := dbOnce()
	if err != nil {
		return "", err
	}

	var record IPRecord
	err = db.Lookup(ip, &record)
	if err != nil {
		return "", err
	}

	// 1. 优先返回当前分配的国家代码 (Country / CountryCode)
	if record.Country.IsoCode != "" {
		return strings.ToLower(record.Country.IsoCode), nil
	}
	if record.CountryCode != "" {
		return strings.ToLower(record.CountryCode), nil
	}

	// 2. 其次回退到注册地或代表国家代码 (Registered / Represented)
	if record.RegisteredCountry.IsoCode != "" {
		return strings.ToLower(record.RegisteredCountry.IsoCode), nil
	}
	if record.RepresentedCountry.IsoCode != "" {
		return strings.ToLower(record.RepresentedCountry.IsoCode), nil
	}

	// 3. 兜底返回大洲代码 (Continent)
	if record.Continent.Code != "" {
		return strings.ToLower(record.Continent.Code), nil
	}
	if record.ContinentCode != "" {
		return strings.ToLower(record.ContinentCode), nil
	}

	return "", errors.New("IP not found")
}
