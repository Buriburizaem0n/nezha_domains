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

type IPInfo struct {
	// 对应数据库里的 "country_code" 字段 (例如 "CN")
	CountryCode string `maxminddb:"country_code"`

	// 对应数据库里的 "continent_code" 字段 (例如 "AS")
	ContinentCode string `maxminddb:"continent_code"`
}

func Lookup(ip net.IP) (string, error) {
	db, err := dbOnce()
	if err != nil {
		return "", err
	}

	var record IPInfo
	err = db.Lookup(ip, &record)
	if err != nil {
		return "", err
	}

	// 1. 优先返回 CountryCode (如 "cn")
	if record.CountryCode != "" {
		// 前端依然需要小写才能匹配 flags/cn.svg
		return strings.ToLower(record.CountryCode), nil
	}

	// 2. 其次返回 ContinentCode
	if record.ContinentCode != "" {
		return strings.ToLower(record.ContinentCode), nil
	}

	return "", errors.New("IP not found")
}
