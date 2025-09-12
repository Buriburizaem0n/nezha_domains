// service/singleton/domain.go
package singleton

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/nezhahq/nezha/model"
	"gorm.io/datatypes"
)

// GetDomains 获取所有域名记录
func GetDomains(scope string) ([]model.Domain, error) {
	var domains []model.Domain
	query := DB

	if scope == "public" {
		// 如果是公开访问，只返回已验证且公开的域名
		query = query.Where("status IN (?, ?) AND is_public = ?", "verified", "expired", true)
	}

	if err := query.Find(&domains).Error; err != nil {
		return nil, err
	}
	return domains, nil
}

// GetDomainByID 根据ID获取单个域名记录
func GetDomainByID(id uint64) (*model.Domain, error) {
	var domain model.Domain
	if err := DB.First(&domain, id).Error; err != nil {
		return nil, err
	}
	return &domain, nil
}

// AddDomain 添加一个新的域名，并自动生成验证Token
func AddDomain(domainName string) (*model.Domain, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("无法生成随机Token: %w", err)
	}
	token := "nezha-verify-" + hex.EncodeToString(b)

	newDomain := &model.Domain{
		Domain:      strings.ToLower(domainName),
		VerifyToken: token,
		Status:      "pending",
	}

	if err := DB.Create(newDomain).Error; err != nil {
		return nil, err
	}

	return newDomain, nil
}

// VerifyDomain 验证域名的 TXT 记录是否正确
func VerifyDomain(id uint64) (bool, error) {
	domain, err := GetDomainByID(id) // 直接调用 GetDomainByID
	if err != nil {
		return false, err
	}
	if domain.Status == "verified" {
		return true, nil
	}

	txtRecords, err := net.LookupTXT(domain.Domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return false, nil
		}
		return false, fmt.Errorf("DNS查询失败: %w", err)
	}

	for _, record := range txtRecords {
		if record == domain.VerifyToken {
			domain.Status = "verified"
			return true, DB.Save(domain).Error
		}
	}

	return false, nil
}

// UpdateDomainConfig 更新指定域名的配置信息
func UpdateDomainConfig(id uint64, billingData datatypes.JSON) (*model.Domain, error) {
	domain, err := GetDomainByID(id) // 直接调用 GetDomainByID
	if err != nil {
		return nil, err
	}

	domain.BillingData = billingData
	if err := DB.Save(domain).Error; err != nil {
		return nil, err
	}
	return domain, nil
}

// UpdateDomain 更新域名信息 (重命名并增强)
func UpdateDomain(id uint64, req model.DomainUpdateRequest) (*model.Domain, error) { // 使用新的请求体
	domain, err := GetDomainByID(id)
	if err != nil {
		return nil, err
	}

	domain.IsPublic = req.IsPublic
	domain.BillingData = req.BillingData
	if err := DB.Save(domain).Error; err != nil {
		return nil, err
	}
	return domain, nil
}

// DeleteDomain 删除一个域名记录
func DeleteDomain(id uint64) error {
	return DB.Delete(&model.Domain{}, id).Error
}

// CronJobForDomainStatus 检查域名到期和自动续费的定时任务
func CronJobForDomainStatus() {
	log.Println("NEZHA>> Cron::开始执行域名状态检查任务")
	var domains []model.Domain
	if err := DB.Where("status = ?", "verified").Find(&domains).Error; err != nil {
		log.Printf("NEZHA>> Cron::Error fetching domains: %v", err)
		return
	}

	now := time.Now()

	for i := range domains {
		d := domains[i]
		if d.BillingData == nil {
			continue
		}

		var billing model.BillingDataMod
		if err := json.Unmarshal(d.BillingData, &billing); err != nil {
			log.Printf("NEZHA>> Cron::Error parsing billing data for domain %s: %v", d.Domain, err)
			continue
		}

		if billing.EndDate == "" {
			continue
		}

		endDate, err := time.Parse(time.RFC3339, billing.EndDate)
		if err != nil {
			log.Printf("NEZHA>> Cron::Error parsing end date for domain %s: %v", d.Domain, err)
			continue
		}

		if now.After(endDate) {
			if billing.AutoRenewal == "1" {
				var newEndDate time.Time
				renewalYears := 0
				renewalMonths := 0
				switch billing.Cycle {
				case "年":
					renewalYears = 1
				case "月":
					renewalMonths = 1
				default:
					log.Printf("NEZHA>> Cron::未知续费周期 '%s' for domain %s", billing.Cycle, d.Domain)
					continue
				}

				newEndDate = endDate.AddDate(renewalYears, renewalMonths, 0)
				billing.EndDate = newEndDate.Format(time.RFC3339)
				newBillingData, _ := json.Marshal(billing)
				d.BillingData = newBillingData
				log.Printf("NEZHA>> Cron::域名 %s 已自动续费至 %s", d.Domain, billing.EndDate)
				if err := DB.Save(&d).Error; err != nil {
					log.Printf("NEZHA>> Cron::Error saving auto-renewed domain %s: %v", d.Domain, err)
				}
			} else {
				d.Status = "expired"
				log.Printf("NEZHA>> Cron::域名 %s 已过期", d.Domain)
				if err := DB.Save(&d).Error; err != nil {
					log.Printf("NEZHA>> Cron::Error marking domain %s as expired: %v", d.Domain, err)
				}
			}
		}
	}
	log.Println("NEZHA>> Cron::域名状态检查任务执行完毕")
}
