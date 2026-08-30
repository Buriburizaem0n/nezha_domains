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
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nezhahq/nezha/model"
	"gorm.io/datatypes"

	whois "github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
)

// SyncDomainPrice 从 哪煮米(nazhumi.com) 获取域名续费价格
func SyncDomainPrice(billing *model.BillingDataMod, domainName string) {
	// 获取 TLD
	parts := strings.Split(domainName, ".")
	if len(parts) < 2 {
		return
	}
	tld := parts[len(parts)-1]

	// 匹配注册商代码 (简单启示式匹配)
	registrarCode := ""
	regNameLower := strings.ToLower(billing.Registrar)

	// 这里可以扩展更多的映射关系
	mapping := map[string]string{
		"aliyun": "aliyun", "tencent": "tencent", "cloudflare": "cloudflare",
		"namesilo": "namesilo", "porkbun": "porkbun", "dynadot": "dynadot",
		"google": "google", "namecheap": "namecheap", "godaddy": "godaddy",
		"spaceship": "spaceship", "huawei": "huawei", "baidu": "baidu",
		"volcengine": "volcengine", "juming": "juming", "quyu": "quyu",
		"west": "west", "xinnet": "xinnet", "ename": "ename",
	}

	for key, code := range mapping {
		if strings.Contains(regNameLower, key) {
			registrarCode = code
			break
		}
	}

	if registrarCode == "" {
		// 备选方案：去除常用后缀和空格
		registrarCode = strings.ReplaceAll(regNameLower, " ", "")
		registrarCode = strings.ReplaceAll(registrarCode, "inc", "")
		registrarCode = strings.ReplaceAll(registrarCode, "llc", "")
		registrarCode = strings.ReplaceAll(registrarCode, ".", "")
		registrarCode = strings.ReplaceAll(registrarCode, ",", "")
	}

	apiURL := fmt.Sprintf("https://www.nazhumi.com/api/v1?registrar=%s&domain=%s", registrarCode, tld)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var results []struct {
		Renew    interface{} `json:"renew"`
		Currency string      `json:"currency"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil || len(results) == 0 {
		return
	}

	// 转换价格
	res := results[0]
	priceStr := ""
	switch v := res.Renew.(type) {
	case float64:
		priceStr = fmt.Sprintf("%.2f", v)
	case string:
		if v != "n/a" {
			priceStr = v
		}
	}

	if priceStr != "" {
		billing.RenewalPrice = fmt.Sprintf("%s %s", priceStr, res.Currency)
	}
}

// RDAPResponse 简化的 RDAP 响应结构
type RDAPResponse struct {
	Events []struct {
		EventAction string `json:"eventAction"`
		EventDate   string `json:"eventDate"`
	} `json:"events"`
	Entities []struct {
		Roles      []string      `json:"roles"`
		VcardArray []interface{} `json:"vcardArray"`
	} `json:"entities"`
}

// SyncDomainWHOIS 使用 RDAP (主要) 和 Whois (备用) 同步域名信息
func SyncDomainWHOIS(d *model.Domain) error {
	var billing model.BillingDataMod
	if d.BillingData != nil && len(d.BillingData) > 0 {
		json.Unmarshal(d.BillingData, &billing)
	}

	// 1. 尝试使用官方 RDAP 协议 (JSON格式，更可靠，无需解析正则)
	rdapSuccess := false
	apiURL := fmt.Sprintf("https://rdap.org/domain/%s", d.Domain)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL)
	if err == nil && resp.StatusCode == http.StatusOK {
		var rdap RDAPResponse
		if err := json.NewDecoder(resp.Body).Decode(&rdap); err == nil {
			rdapSuccess = true
			for _, event := range rdap.Events {
				switch event.EventAction {
				case "expiration":
					billing.EndDate = event.EventDate
				case "registration":
					billing.RegisteredDate = event.EventDate
				}
			}
			// 提取注册商
			for _, entity := range rdap.Entities {
				isRegistrar := false
				for _, role := range entity.Roles {
					if role == "registrar" {
						isRegistrar = true
						break
					}
				}
				if isRegistrar && len(entity.VcardArray) > 1 {
					if vcard, ok := entity.VcardArray[1].([]interface{}); ok {
						for _, field := range vcard {
							if f, ok := field.([]interface{}); ok && len(f) > 3 {
								if f[0] == "fn" {
									billing.Registrar = fmt.Sprint(f[3])
									break
								}
							}
						}
					}
				}
			}
		}
		resp.Body.Close()
	}

	// 2. 如果 RDAP 失败，回退到传统的 Whois 查询
	if !rdapSuccess {
		raw, err := whois.Whois(d.Domain)
		if err == nil {
			result, err := whoisparser.Parse(raw)
			if err == nil {
				if result.Registrar.Name != "" {
					billing.Registrar = result.Registrar.Name
				}
				if result.Domain.ExpirationDate != "" {
					billing.EndDate = result.Domain.ExpirationDate
				}
				if result.Domain.CreatedDate != "" {
					billing.RegisteredDate = result.Domain.CreatedDate
				}
			}
		}
	}

	// 3. 补充价格同步
	SyncDomainPrice(&billing, d.Domain)

	newBillingData, err := json.Marshal(billing)
	if err != nil {
		return err
	}

	d.BillingData = newBillingData
	saveErr := DB.Save(d).Error
	if saveErr != nil {
		return fmt.Errorf("数据库保存失败: %w", saveErr)
	}

	if !rdapSuccess && billing.EndDate == "" {
		return fmt.Errorf("RDAP 和 Whois 同步均失败，请检查网络或手动输入")
	}

	return nil
}

// SyncAllDomains 异步批量同步所有已验证域名的 Whois 和价格信息
func SyncAllDomains() {
	go func() {
		log.Println("NEZHA>> 开始批量同步所有域名的 Whois 和价格信息...")
		domains, err := GetDomains("admin")
		if err != nil {
			log.Printf("NEZHA>> 批量同步域名失败: %v", err)
			return
		}

		successCount := 0
		for _, d := range domains {
			if d.Status == "verified" {
				if err := SyncDomainWHOIS(&d); err != nil {
					log.Printf("NEZHA>> 域名 %s 同步失败: %v", d.Domain, err)
				} else {
					successCount++
				}
				// 避免并发过高被 API 限制
				time.Sleep(2 * time.Second)
			}
		}
		log.Printf("NEZHA>> 批量同步域名结束，成功 %d/%d", successCount, len(domains))
	}()
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

func isDomainNotificationDay(daysLeft int) bool {
	daysStr := Conf.DomainExpiryNotificationDays
	if daysStr == "" {
		return slices.Contains([]int{60, 30, 15, 7, 3, 1, 0}, daysLeft+1)
	}
	parts := strings.Split(daysStr, ",")
	for _, p := range parts {
		d, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil && (d == daysLeft+1 || (d == 0 && daysLeft == 0)) {
			return true
		}
	}
	return false
}

func isServerNotificationDay(daysLeft int) bool {
	daysStr := Conf.ServerExpiryNotificationDays
	if daysStr == "" {
		return slices.Contains([]int{30, 15, 7, 3, 1, 0}, daysLeft+1)
	}
	parts := strings.Split(daysStr, ",")
	for _, p := range parts {
		d, err := strconv.Atoi(strings.TrimSpace(p))
		if err == nil && (d == daysLeft+1 || (d == 0 && daysLeft == 0)) {
			return true
		}
	}
	return false
}

func isAutoRenewal(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case string:
		s := strings.ToLower(strings.TrimSpace(val))
		return s == "1" || s == "true" || s == "auto" || s == "yes" || s == "on"
	case bool:
		return val
	case float64:
		return val == 1
	case int:
		return val == 1
	case int64:
		return val == 1
	}
	return false
}

func advanceRenewalDate(startDateStr, endDateStr, cycle string, now time.Time) (newStartStr, newEndStr string, newEndDate time.Time, renewed bool) {
	endDate, err := ParseFlexibleDate(endDateStr)
	if err != nil || !now.After(endDate) {
		return startDateStr, endDateStr, endDate, false
	}

	startDate, _ := ParseFlexibleDate(startDateStr)

	cycleLower := strings.ToLower(strings.TrimSpace(cycle))
	var years, months, days int
	switch cycleLower {
	case "day", "天", "日", "1day", "1天", "1日":
		days = 1
	case "week", "周", "星期", "1week", "1周":
		days = 7
	case "month", "月", "1month", "1月", "按月":
		months = 1
	case "quarter", "季", "季度", "3month", "3月", "按季":
		months = 3
	case "halfyear", "半年", "6month", "6月", "半年度":
		months = 6
	case "year", "年", "1year", "1年", "按年", "每年":
		years = 1
	case "2year", "2年", "两年":
		years = 2
	case "3year", "3年", "三年":
		years = 3
	case "5year", "5年", "五年":
		years = 5
	default:
		if !startDate.IsZero() && endDate.After(startDate) {
			duration := endDate.Sub(startDate)
			curEnd := endDate
			curStart := startDate
			for !curEnd.After(now) {
				curStart = curEnd
				curEnd = curEnd.Add(duration)
			}
			newEndDate = curEnd
			return formatLikeOriginal(startDateStr, curStart), formatLikeOriginal(endDateStr, curEnd), newEndDate, true
		}
		years = 1 // 默认按年
	}

	curEnd := endDate
	curStart := startDate
	for !curEnd.After(now) {
		curStart = curEnd
		curEnd = curEnd.AddDate(years, months, days)
	}

	newEndDate = curEnd
	newStartStr = formatLikeOriginal(startDateStr, curStart)
	newEndStr = formatLikeOriginal(endDateStr, curEnd)
	return newStartStr, newEndStr, newEndDate, true
}

func formatLikeOriginal(original string, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if len(original) == 10 && !strings.Contains(original, "T") {
		return t.Format("2006-01-02")
	}
	if len(original) == 19 && strings.Contains(original, " ") {
		return t.Format("2006-01-02 15:04:05")
	}
	return t.Format(time.RFC3339)
}

// ParseFlexibleDate 解析多种日期格式 (YYYY-MM-DD, YYYY-MM-DD HH:MM:SS, RFC3339)
func ParseFlexibleDate(dateStr string) (time.Time, error) {
	if len(dateStr) == 10 { // YYYY-MM-DD
		return time.Parse("2006-01-02", dateStr)
	} else if len(dateStr) == 19 && dateStr[10] == ' ' { // YYYY-MM-DD HH:MM:SS
		return time.Parse("2006-01-02 15:04:05", dateStr)
	}
	return time.Parse(time.RFC3339, dateStr)
}

// CronJobForDomainStatus 检查域名到期和自动续费的定时任务
func CronJobForDomainStatus() {
	log.Println("NEZHA>> Cron::开始执行域名状态检查任务")
	var domains []model.Domain
	if err := DB.Find(&domains).Error; err != nil {
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

		endDate, err := ParseFlexibleDate(billing.EndDate)
		if err != nil {
			log.Printf("NEZHA>> Cron::Error parsing end date for domain %s: %v", d.Domain, err)
			continue
		}

		if isAutoRenewal(billing.AutoRenewal) && now.After(endDate) {
			newStartStr, newEndStr, newEnd, renewed := advanceRenewalDate(billing.RegisteredDate, billing.EndDate, billing.Cycle, now)
			if renewed {
				billing.EndDate = newEndStr
				if billing.RegisteredDate != "" {
					billing.RegisteredDate = newStartStr
				}
				newBillingData, _ := json.Marshal(billing)
				d.BillingData = newBillingData
				endDate = newEnd
				log.Printf("NEZHA>> Cron::域名 %s 开启了自动续费，已自动顺延至 %s", d.Domain, billing.EndDate)
				if err := DB.Save(&d).Error; err != nil {
					log.Printf("NEZHA>> Cron::Error saving auto-renewed domain %s: %v", d.Domain, err)
				}
			}
		} else if now.After(endDate) {
			d.Status = "expired"
			log.Printf("NEZHA>> Cron::域名 %s 已过期", d.Domain)
			if err := DB.Save(&d).Error; err != nil {
				log.Printf("NEZHA>> Cron::Error marking domain %s as expired: %v", d.Domain, err)
			}
		}

		daysLeft := int(endDate.Sub(now).Hours() / 24)

		if isDomainNotificationDay(daysLeft) {
			msg := ""
			if daysLeft+1 > 0 {
				msg = fmt.Sprintf("域名 [%s] 即将到期，剩余 %d 天。到期时间: %s", d.Domain, daysLeft+1, endDate.Format("2006-01-02"))
			} else {
				msg = fmt.Sprintf("域名 [%s] 已到期！到期时间: %s", d.Domain, endDate.Format("2006-01-02"))
			}
			if Conf.ExpiryNotificationGroupID != 0 {
				NotificationShared.SendNotification(Conf.ExpiryNotificationGroupID, msg, fmt.Sprintf("expiry-domain-%d-%d", d.ID, daysLeft))
			}
			SendTGAdminNotification("🌐 <b>域名到期提醒</b>\n\n" + msg)
			if model.SendGlobalEmailFunc != nil {
				_ = model.SendGlobalEmailFunc(msg)
			}
		}
	}
	log.Println("NEZHA>> Cron::域名状态检查任务执行完毕")
}

// CronJobForServerStatus 检查服务器/VPS 到期通知与自动续费滚动
func CronJobForServerStatus() {
	log.Println("NEZHA>> Cron::开始执行服务器到期检查任务")
	var servers []model.Server
	if err := DB.Find(&servers).Error; err != nil {
		log.Printf("NEZHA>> Cron::Error fetching servers: %v", err)
		return
	}

	now := time.Now()

	for i := range servers {
		s := &servers[i]

		var publicNoteObj map[string]any
		var noteObj map[string]any
		var billingMap map[string]any
		isPublicNote := false
		isPrivateNote := false

		if s.PublicNote != "" {
			if err := json.Unmarshal([]byte(s.PublicNote), &publicNoteObj); err == nil && publicNoteObj != nil {
				if bm, ok := publicNoteObj["billingDataMod"].(map[string]any); ok && bm != nil {
					billingMap = bm
					isPublicNote = true
				}
			}
		}

		if billingMap == nil && s.Note != "" {
			if err := json.Unmarshal([]byte(s.Note), &noteObj); err == nil && noteObj != nil {
				if bm, ok := noteObj["billingDataMod"].(map[string]any); ok && bm != nil {
					billingMap = bm
					isPrivateNote = true
				}
			}
		}

		if billingMap == nil {
			continue
		}

		endDateStr, _ := billingMap["endDate"].(string)
		if endDateStr == "" || strings.HasPrefix(endDateStr, "0000-00-00") {
			continue
		}

		startDateStr, _ := billingMap["startDate"].(string)
		cycle, _ := billingMap["cycle"].(string)
		autoRenewalVal := billingMap["autoRenewal"]

		endDate, err := ParseFlexibleDate(endDateStr)
		if err != nil {
			log.Printf("NEZHA>> Cron::Error parsing end date for VPS %s: %v", s.Name, err)
			continue
		}

		// 如果开启了自动续费且已到达或超过到期时间，自动将周期顺延至未来的有效周期
		if isAutoRenewal(autoRenewalVal) && now.After(endDate) {
			newStartStr, newEndStr, newEnd, renewed := advanceRenewalDate(startDateStr, endDateStr, cycle, now)
			if renewed {
				billingMap["endDate"] = newEndStr
				if startDateStr != "" {
					billingMap["startDate"] = newStartStr
				}
				endDate = newEnd

				if isPublicNote {
					publicNoteObj["billingDataMod"] = billingMap
					if updatedJSON, err := json.Marshal(publicNoteObj); err == nil {
						s.PublicNote = string(updatedJSON)
					}
				} else if isPrivateNote {
					noteObj["billingDataMod"] = billingMap
					if updatedJSON, err := json.Marshal(noteObj); err == nil {
						s.Note = string(updatedJSON)
					}
				}

				if err := DB.Save(s).Error; err != nil {
					log.Printf("NEZHA>> Cron::Error saving auto-renewed VPS %s: %v", s.Name, err)
				} else {
					ServerShared.Update(s, s.UUID)
					log.Printf("NEZHA>> Cron::VPS [%s] 开启了自动续费，到期时间已自动顺延至 %s (周期: %s)", s.Name, newEndStr, cycle)
				}
			}
		}

		daysLeft := int(endDate.Sub(now).Hours() / 24)

		if isServerNotificationDay(daysLeft) {
			msg := ""
			if daysLeft+1 > 0 {
				msg = fmt.Sprintf("VPS [%s] 即将到期，剩余 %d 天。到期时间: %s", s.Name, daysLeft+1, endDate.Format("2006-01-02"))
			} else {
				msg = fmt.Sprintf("VPS [%s] 已到期！到期时间: %s", s.Name, endDate.Format("2006-01-02"))
			}
			if Conf.ExpiryNotificationGroupID != 0 {
				NotificationShared.SendNotification(Conf.ExpiryNotificationGroupID, msg, fmt.Sprintf("expiry-server-%d-%d", s.ID, daysLeft), s)
			}

			SendTGAdminNotification("🖥 <b>VPS 到期提醒</b>\n\n" + msg)
			if model.SendGlobalEmailFunc != nil {
				_ = model.SendGlobalEmailFunc(msg)
			}
		}
	}
	log.Println("NEZHA>> Cron::服务器到期检查任务执行完毕")
}
