package singleton

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nezhahq/nezha/model"
	"github.com/nezhahq/nezha/pkg/utils"
)

type tgUpdate struct {
	UpdateID int `json:"update_id"`
	Message  *struct {
		MessageID int `json:"message_id"`
		From      *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Chat *struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

func InitTelegramBot() {
	if Conf.TelegramBotToken == "" {
		log.Println("NEZHA>> TG Bot Token 未配置，跳过启动互动机器人")
		return
	}

	log.Println("NEZHA>> 正在启动 Telegram 互动机器人...")
	go func() {
		offset := 0
		for {
			updates, err := getTGUpdates(Conf.TelegramBotToken, offset)
			if err != nil {
				// 避免过于频繁报错
				time.Sleep(30 * time.Second)
				continue
			}

			for _, update := range updates {
				offset = update.UpdateID + 1
				if update.Message != nil {
					handleTGUpdate(update)
				}
			}
			time.Sleep(3 * time.Second)
		}
	}()
}

func getTGUpdates(token string, offset int) ([]tgUpdate, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=20", token, offset)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := utils.HttpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool       `json:"ok"`
		Result []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Result, nil
}

func handleTGUpdate(update tgUpdate) {
	if update.Message == nil || update.Message.Chat == nil {
		return
	}

	chatID := update.Message.Chat.ID
	adminChatID, _ := strconv.ParseInt(Conf.TelegramAdminChatID, 10, 64)

	// 权限检查
	if adminChatID != 0 && chatID != adminChatID {
		sendTGMessage(chatID, "🚫 您没有权限操作此机器人。")
		return
	}

	text := update.Message.Text
	switch {
	case text == "/start" || text == "/help":
		sendTGMainMenu(chatID)
	case text == "/status" || text == "📊 运行状态":
		sendTGStatus(chatID)
	case text == "/domains" || text == "🌐 域名监控":
		sendTGDomains(chatID)
	default:
		if strings.HasPrefix(text, "/") {
			sendTGMessage(chatID, "❓ 未知命令，请输入 /start 查看菜单。")
		}
	}
}

func sendTGMainMenu(chatID int64) {
	menu := "👋 您好！我是哪吒监控助手。\n\n请选择以下操作："
	keyboard := map[string]interface{}{
		"keyboard": [][]map[string]string{
			{{"text": "📊 运行状态"}, {"text": "🌐 域名监控"}},
		},
		"resize_keyboard": true,
	}
	kbJSON, _ := json.Marshal(keyboard)
	sendTGRequest("sendMessage", url.Values{
		"chat_id":      {strconv.FormatInt(chatID, 10)},
		"text":         {menu},
		"reply_markup": {string(kbJSON)},
	})
}

func sendTGStatus(chatID int64) {
	var sb strings.Builder
	sb.WriteString("📊 <b>服务器实时状态</b>\n\n")

	ServerShared.Range(func(id uint64, s *model.Server) bool {
		statusIcon := "🟢"
		if !s.LastActive.After(time.Now().Add(-time.Second * 30)) {
			statusIcon = "🔴"
		}
		sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n", statusIcon, s.Name))
		sb.WriteString(fmt.Sprintf("├ CPU: %.1f%% | Mem: %.1f%%\n", s.State.CPU, float64(s.State.MemUsed)/float64(s.Host.MemTotal)*100))
		sb.WriteString(fmt.Sprintf("└ Net: ↓%s/s ↑%s/s\n\n", utils.Bytes(s.State.NetInSpeed), utils.Bytes(s.State.NetOutSpeed)))
		return true
	})

	if sb.Len() < 50 {
		sb.WriteString("暂无在线服务器。")
	}

	sendTGMessage(chatID, sb.String())
}

func sendTGDomains(chatID int64) {
	domains, err := GetDomains("admin")
	if err != nil {
		sendTGMessage(chatID, "❌ 获取域名列表失败。")
		return
	}

	var sb strings.Builder
	sb.WriteString("🌐 <b>域名监控状态</b>\n\n")

	now := time.Now()
	for _, d := range domains {
		statusIcon := "✅"
		if d.Status == "pending" {
			statusIcon = "⏳"
		} else if d.Status == "expired" {
			statusIcon = "❌"
		}

		expiresInfo := "N/A"
		if d.BillingData != nil {
			var billing model.BillingDataMod
			if json.Unmarshal(d.BillingData, &billing) == nil && billing.EndDate != "" {
				if endDate, err := time.Parse(time.RFC3339, billing.EndDate); err == nil {
					daysLeft := int(endDate.Sub(now).Hours() / 24)
					expiresInfo = fmt.Sprintf("%d 天", daysLeft)
				}
			}
		}

		sb.WriteString(fmt.Sprintf("%s <b>%s</b>\n", statusIcon, d.Domain))
		sb.WriteString(fmt.Sprintf("└ 剩余: %s | 状态: %s\n\n", expiresInfo, d.Status))
	}

	if len(domains) == 0 {
		sb.WriteString("暂无监控中的域名。")
	}

	sendTGMessage(chatID, sb.String())
}

func sendTGMessage(chatID int64, text string) {
	sendTGRequest("sendMessage", url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"text":       {text},
		"parse_mode": {"HTML"},
	})
}

func sendTGRequest(method string, params url.Values) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/%s", Conf.TelegramBotToken, method)
	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(params.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := utils.HttpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}
