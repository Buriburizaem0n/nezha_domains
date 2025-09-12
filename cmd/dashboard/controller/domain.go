// cmd/dashboard/controller/domain.go
package controller

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nezhahq/nezha/model"
	"github.com/nezhahq/nezha/service/singleton"
)

// DomainAPIResponse 是用于API返回的结构体，可以包含一些后端计算好的字段
type DomainAPIResponse struct {
	model.Domain
	ExpiresInDays *int `json:"expires_in_days"` // 剩余天数，使用指针以区分0和null
}

// UpdateDomain 更新域名
func UpdateDomain(c *gin.Context) (any, error) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return nil, newGormError("无效的域名ID")
	}
	var req model.DomainUpdateRequest // 使用新的请求体
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, newGormError("无效的请求: %s", err.Error())
	}

	return singleton.UpdateDomain(domainID, req)
}

func GetDomainList(c *gin.Context) (any, error) {
	scope := c.DefaultQuery("scope", "admin")
	domains, err := singleton.GetDomains(scope)
	if err != nil {
		return nil, err
	}

	var response []DomainAPIResponse
	for _, d := range domains {
		apiDomain := DomainAPIResponse{
			Domain: d,
		}
		if d.BillingData != nil {
			var billing model.BillingDataMod
			if json.Unmarshal(d.BillingData, &billing) == nil && billing.EndDate != "" {
				if endDate, err := time.Parse(time.RFC3339, billing.EndDate); err == nil {
					daysLeft := int(time.Until(endDate).Hours() / 24)
					apiDomain.ExpiresInDays = &daysLeft
				}
			}
		}
		response = append(response, apiDomain)
	}

	return response, nil
}

func AddDomain(c *gin.Context) (any, error) {
	var req model.DomainAPIRequest // 使用 model/domain.go 中定义的请求体
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, newGormError("无效的请求: %s", err.Error())
	}
	return singleton.AddDomain(req.Domain)
}

func VerifyDomain(c *gin.Context) (any, error) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return nil, newGormError("无效的域名ID")
	}

	success, err := singleton.VerifyDomain(domainID)
	if err != nil {
		return nil, newGormError("验证过程中发生错误: %s", err.Error())
	}

	var message string
	if success {
		message = "验证成功，域名状态已更新"
	} else {
		message = "验证失败，未找到匹配的 TXT 记录"
	}

	// 对于这种包含非数据字段（如message）的特殊成功响应，
	// 我们可以直接返回一个 map，commonHandler 会将其包装
	return gin.H{
		"success": success,
		"message": message,
	}, nil
}

func DeleteDomain(c *gin.Context) (any, error) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return nil, nil // ID无效，直接返回成功，不做任何事
	}

	if err := singleton.DeleteDomain(domainID); err != nil {
		return nil, err
	}
	// 对于DELETE成功，通常不返回data，返回nil即可
	return nil, nil
}

func UpdateDomainInfo(c *gin.Context) (any, error) {
	domainID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		return nil, newGormError("无效的域名ID")
	}

	var req model.DomainUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		return nil, newGormError("无效的请求: %s", err.Error())
	}

	return singleton.UpdateDomain(domainID, req)
}
