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

	whois "github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
)


// SyncDomainWHOIS 从 Whois 获取域名信息并同步到 BillingData
func SyncDomainWHOIS(d *model.Domain) error {
	raw, err := whois.Whois(d.Domain)
	if err != nil {
		return fmt.Errorf("Whois查询失败: %w", err)
	}

	result, err := whoisparser.Parse(raw)
	if err != nil {
		return fmt.Errorf("Whois解析失败: %w", err)
	}

	var billing model.BillingDataMod
	if d.BillingData != nil && len(d.BillingData) > 0 {
		json.Unmarshal(d.BillingData, &billing)
	}

	// 填充信息
	if result.Registrar.Name != "" {
		billing.Registrar = result.Registrar.Name
	}
	if result.Domain.ExpirationDate != "" {
		billing.EndDate = result.Domain.ExpirationDate
	}
	if result.Domain.CreatedDate != "" {
		billing.RegisteredDate = result.Domain.CreatedDate
	}

	newBillingData, err := json.Marshal(billing)
	if err != nil {
		return err
	}

	d.BillingData = newBillingData
	return DB.Save(d).Error
}

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

	found := false
	for _, record := range txtRecords {
		if record == domain.VerifyToken {
			domain.Status = "verified"
			found = true
			break
		}
	}

	if found {
		// 自动同步 Whois 信息
		if err := SyncDomainWHOIS(domain); err != nil {
			log.Printf("NEZHA>> 域名 %s 验证成功但 Whois 同步失败: %v", domain.Domain, err)
		}
		return true, DB.Save(domain).Error
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

		daysLeft := int(endDate.Sub(now).Hours() / 24)

		// 只有在到期前一定天数通知，且避开重复通知 (简单逻辑：每天通知一次)
		if Conf.ExpiryNotificationGroupID != 0 {
			msg := ""
			switch daysLeft + 1 {
			case 60, 30, 15, 7, 3, 1:
				msg = fmt.Sprintf("域名 [%s] 即将到期，剩余 %d 天。到期时间: %s", d.Domain, daysLeft+1, endDate.Format("2006-01-02"))
			case 0:
				msg = fmt.Sprintf("域名 [%s] 已到期！到期时间: %s", d.Domain, endDate.Format("2006-01-02"))
			}
			if msg != "" {
				NotificationShared.SendNotification(Conf.ExpiryNotificationGroupID, msg, fmt.Sprintf("expiry-domain-%d-%d", d.ID, daysLeft))
			}
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

// CronJobForServerStatus 检查服务器/VPS 到期通知
func CronJobForServerStatus() {
	log.Println("NEZHA>> Cron::开始执行服务器到期检查任务")
	var servers []model.Server
	if err := DB.Find(&servers).Error; err != nil {
		log.Printf("NEZHA>> Cron::Error fetching servers: %v", err)
		return
	}

	now := time.Now()

	for i := range servers {
		s := servers[i]
		if s.BillingData == nil {
			continue
		}

		var billing model.BillingDataMod
		if err := json.Unmarshal(s.BillingData, &billing); err != nil {
			continue
		}

		if billing.EndDate == "" {
			continue
		}

		endDate, err := time.Parse(time.RFC3339, billing.EndDate)
		if err != nil {
			continue
		}

		daysLeft := int(endDate.Sub(now).Hours() / 24)

		if Conf.ExpiryNotificationGroupID != 0 {
			msg := ""
			switch daysLeft + 1 {
			case 30, 15, 7, 3, 1:
				msg = fmt.Sprintf("VPS [%s] 即将到期，剩余 %d 天。到期时间: %s", s.Name, daysLeft+1, endDate.Format("2006-01-02"))
			case 0:
				msg = fmt.Sprintf("VPS [%s] 已到期！到期时间: %s", s.Name, endDate.Format("2006-01-02"))
			}
			if msg != "" {
				NotificationShared.SendNotification(Conf.ExpiryNotificationGroupID, msg, fmt.Sprintf("expiry-server-%d-%d", s.ID, daysLeft))
			}
		}
	}
	log.Println("NEZHA>> Cron::服务器到期检查任务执行完毕")
}
