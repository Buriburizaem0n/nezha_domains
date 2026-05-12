package singleton

import (
	"encoding/json"
	"fmt"
	"io"
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
	UpdateID      int `json:"update_id"`
	Message       *tgMessage `json:"message"`
	CallbackQuery *tgCallbackQuery `json:"callback_query"`
}

type tgMessage struct {
	MessageID int `json:"message_id"`
	From      *struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Chat *struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text string `json:"text"`
}

type tgCallbackQuery struct {
	ID      string     `json:"id"`
	From    struct {
		ID int64 `json:"id"`
	} `json:"from"`
	Message *tgMessage `json:"message"`
	Data    string     `json:"data"`
}

func InitTelegramBot() {
	log.Printf("NEZHA>> InitTelegramBot called. Token length: %d", len(Conf.TelegramBotToken))
	if Conf.TelegramBotToken == "" {
		log.Println("NEZHA>> TG Bot Token 未配置，跳过启动互动机器人")
		return
	}

	log.Println("NEZHA>> 正在启动 Telegram 互动机器人...")
	
	// 在启动前删除可能存在的 Webhook，防止 getUpdates 冲突
	deleteWebhookURL := fmt.Sprintf("https://api.telegram.org/bot%s/deleteWebhook?drop_pending_updates=true", Conf.TelegramBotToken)
	if req, err := http.NewRequest(http.MethodPost, deleteWebhookURL, nil); err == nil {
		if resp, err := utils.HttpClient.Do(req); err == nil {
			log.Printf("NEZHA>> 尝试删除 Webhook 完毕，状态码: %d", resp.StatusCode)
			resp.Body.Close()
		} else {
			log.Printf("NEZHA>> 删除 Webhook 失败: %v", err)
		}
	}

	go func() {
		offset := 0
		for {
			updates, err := getTGUpdates(Conf.TelegramBotToken, offset)
			if err != nil {
				log.Printf("NEZHA>> 获取 TG Bot 更新失败: %v", err)
				// 避免过于频繁报错
				time.Sleep(10 * time.Second)
				continue
			}

			if len(updates) > 0 {
				log.Printf("NEZHA>> 收到了 %d 条 TG Bot 更新", len(updates))
			}

			for _, update := range updates {
				offset = update.UpdateID + 1
				handleTGUpdate(update)
			}
			time.Sleep(2 * time.Second)
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
		OK          bool       `json:"ok"`
		Description string     `json:"description"`
		Result      []tgUpdate `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %v", err)
	}
	if !result.OK {
		return nil, fmt.Errorf("Telegram API error: %s", result.Description)
	}
	return result.Result, nil
}

func handleTGUpdate(update tgUpdate) {
	var chatID int64
	var text string
	var messageID int

	if update.Message != nil {
		chatID = update.Message.Chat.ID
		text = update.Message.Text
		messageID = update.Message.MessageID
	} else if update.CallbackQuery != nil {
		chatID = update.CallbackQuery.Message.Chat.ID
		text = update.CallbackQuery.Data
		messageID = update.CallbackQuery.Message.MessageID
		answerTGCallbackQuery(update.CallbackQuery.ID)
	} else {
		return
	}

	adminChatID, _ := strconv.ParseInt(Conf.TelegramAdminChatID, 10, 64)

	// 权限检查
	if adminChatID != 0 && chatID != adminChatID {
		log.Printf("NEZHA>> [TG Bot] 拒绝了来自 ChatID %d 的请求", chatID)
		sendTGMessage(chatID, "🚫 您没有权限操作此机器人。")
		return
	}

	log.Printf("NEZHA>> [TG Bot] 处理命令: %s", text)
	switch {
	case text == "/start" || text == "/help":
		sendTGMainMenu(chatID)
	case text == "/status" || text == "📊 运行状态":
		sendTGServerList(chatID, 0, 0)
	case strings.HasPrefix(text, "sl:"):
		// sl:page
		page, _ := strconv.Atoi(strings.TrimPrefix(text, "sl:"))
		sendTGServerList(chatID, page, messageID)
	case strings.HasPrefix(text, "sd:"):
		// sd:serverID
		sid, _ := strconv.ParseUint(strings.TrimPrefix(text, "sd:"), 10, 64)
		sendTGServerDetail(chatID, sid, messageID)
	case text == "/domains" || text == "🌐 域名监控":
		sendTGDomains(chatID)
	default:
		if strings.HasPrefix(text, "/") {
			sendTGMessage(chatID, "❓ 未知命令，请输入 /start 查看菜单。")
		}
	}
}

func answerTGCallbackQuery(callbackQueryID string) {
	sendTGRequest("answerCallbackQuery", url.Values{
		"callback_query_id": {callbackQueryID},
	})
}

func sendTGServerList(chatID int64, page int, messageID int) {
	allServers := ServerShared.GetSortedList()

	pageSize := 10
	totalPages := (len(allServers) + pageSize - 1) / pageSize
	if page < 0 {
		page = 0
	}
	if page >= totalPages && totalPages > 0 {
		page = totalPages - 1
	}

	start := page * pageSize
	end := start + pageSize
	if end > len(allServers) {
		end = len(allServers)
	}

	var sb strings.Builder
	sb.WriteString("📊 <b>服务器列表</b>")
	if totalPages > 1 {
		sb.WriteString(fmt.Sprintf(" (第 %d/%d 页)", page+1, totalPages))
	}
	sb.WriteString("\n\n请选择服务器查看详情：")

	var keyboard [][]map[string]string
	for i := start; i < end; i++ {
		s := allServers[i]
		statusIcon := "🟢"
		if !s.LastActive.After(time.Now().Add(-time.Second * 30)) {
			statusIcon = "🔴"
		}
		keyboard = append(keyboard, []map[string]string{
			{
				"text":          fmt.Sprintf("%s %s", statusIcon, s.Name),
				"callback_data": fmt.Sprintf("sd:%d", s.ID),
			},
		})
	}

	// 导航按钮
	var navRow []map[string]string
	if page > 0 {
		navRow = append(navRow, map[string]string{"text": "⬅️ 上一页", "callback_data": fmt.Sprintf("sl:%d", page-1)})
	}
	if totalPages > 1 {
		navRow = append(navRow, map[string]string{"text": fmt.Sprintf("%d/%d", page+1, totalPages), "callback_data": "none"})
	}
	if page < totalPages-1 {
		navRow = append(navRow, map[string]string{"text": "下一页 ➡️", "callback_data": fmt.Sprintf("sl:%d", page+1)})
	}
	if len(navRow) > 0 {
		keyboard = append(keyboard, navRow)
	}

	kbJSON, _ := json.Marshal(map[string]interface{}{"inline_keyboard": keyboard})
	
	method := "sendMessage"
	params := url.Values{
		"chat_id":      {strconv.FormatInt(chatID, 10)},
		"text":         {sb.String()},
		"parse_mode":   {"HTML"},
		"reply_markup": {string(kbJSON)},
	}
	if messageID != 0 {
		method = "editMessageText"
		params.Add("message_id", strconv.Itoa(messageID))
	}
	sendTGRequest(method, params)
}

func sendTGServerDetail(chatID int64, serverID uint64, messageID int) {
	s, ok := ServerShared.Get(serverID)
	if !ok {
		sendTGMessage(chatID, "❌ 找不到该服务器。")
		return
	}

	statusIcon := "🟢 在线"
	if !s.LastActive.After(time.Now().Add(-time.Second * 30)) {
		statusIcon = "🔴 离线"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🖥 <b>服务器详情: %s</b>\n", s.Name))
	sb.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━\n"))
	sb.WriteString(fmt.Sprintf("状态: %s\n", statusIcon))
	sb.WriteString(fmt.Sprintf("系统: %s-%s (%s)\n", s.Host.Platform, s.Host.PlatformVersion, s.Host.Arch))
	
	// 计费信息
	var noteData struct {
		BillingDataMod struct {
			EndDate string `json:"endDate"`
			Amount  string `json:"amount"`
			Cycle   string `json:"cycle"`
		} `json:"billingDataMod"`
	}
	if (s.Note != "" && json.Unmarshal([]byte(s.Note), &noteData) == nil && noteData.BillingDataMod.EndDate != "") ||
	   (s.PublicNote != "" && json.Unmarshal([]byte(s.PublicNote), &noteData) == nil && noteData.BillingDataMod.EndDate != "") {
		if endDate, err := time.Parse(time.RFC3339, noteData.BillingDataMod.EndDate); err == nil {
			daysLeft := int(endDate.Sub(time.Now()).Hours() / 24)
			sb.WriteString(fmt.Sprintf("到期: %s (%d天后)\n", endDate.Format("2006-01-02"), daysLeft))
			if noteData.BillingDataMod.Amount != "" {
				sb.WriteString(fmt.Sprintf("续费: %s / %s\n", noteData.BillingDataMod.Amount, noteData.BillingDataMod.Cycle))
			}
		}
	}

	sb.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━\n"))
	sb.WriteString(fmt.Sprintf("CPU: %.1f%% (%d 核)\n", s.State.CPU, len(s.Host.CPU)))
	sb.WriteString(fmt.Sprintf("内存: %.1f%% (%s / %s)\n", float64(s.State.MemUsed)/float64(s.Host.MemTotal)*100, utils.Bytes(s.State.MemUsed), utils.Bytes(s.Host.MemTotal)))
	if s.Host.SwapTotal > 0 {
		sb.WriteString(fmt.Sprintf("交换: %.1f%% (%s / %s)\n", float64(s.State.SwapUsed)/float64(s.Host.SwapTotal)*100, utils.Bytes(s.State.SwapUsed), utils.Bytes(s.Host.SwapTotal)))
	}
	sb.WriteString(fmt.Sprintf("硬盘: %.1f%% (%s / %s)\n", float64(s.State.DiskUsed)/float64(s.Host.DiskTotal)*100, utils.Bytes(s.State.DiskUsed), utils.Bytes(s.Host.DiskTotal)))
	sb.WriteString(fmt.Sprintf("━━━━━━━━━━━━━━━\n"))
	sb.WriteString(fmt.Sprintf("负载: %.2f / %.2f / %.2f\n", s.State.Load1, s.State.Load5, s.State.Load15))
	sb.WriteString(fmt.Sprintf("流量: ↓%s ↑%s\n", utils.Bytes(s.State.NetInTransfer), utils.Bytes(s.State.NetOutTransfer)))
	sb.WriteString(fmt.Sprintf("网速: ↓%s/s ↑%s/s\n", utils.Bytes(s.State.NetInSpeed), utils.Bytes(s.State.NetOutSpeed)))
	sb.WriteString(fmt.Sprintf("活跃: %s\n", s.LastActive.In(Loc).Format("2006-01-02 15:04:05")))

	keyboard := [][]map[string]string{
		{{"text": "🔙 返回列表", "callback_data": "sl:0"}},
	}
	kbJSON, _ := json.Marshal(map[string]interface{}{"inline_keyboard": keyboard})

	sendTGRequest("editMessageText", url.Values{
		"chat_id":      {strconv.FormatInt(chatID, 10)},
		"message_id":   {strconv.Itoa(messageID)},
		"text":         {sb.String()},
		"parse_mode":   {"HTML"},
		"reply_markup": {string(kbJSON)},
	})
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
	log.Printf("NEZHA>> [TG Bot] 准备发送消息到 ChatID %d，长度: %d", chatID, len(text))
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
		log.Printf("NEZHA>> [TG Bot] 创建 HTTP 请求失败: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := utils.HttpClient.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("NEZHA>> [TG Bot] 请求 %s 失败，状态码: %d, 返回内容: %s", method, resp.StatusCode, string(body))
		} else {
			log.Printf("NEZHA>> [TG Bot] 请求 %s 成功", method)
		}
	} else {
		log.Printf("NEZHA>> [TG Bot] 发送请求 %s 失败: %v", method, err)
	}
}
