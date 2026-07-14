package tenderduty

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type addChainSession struct {
	step    int
	name    string
	chainID string
	valoper string
	rpcURL  string
	lcdURL  string
}

type deleteChainSession struct {
	chainName string
}

var (
	addChainSessions      = map[int64]*addChainSession{}
	addChainSessionMux    sync.Mutex
	deleteChainSessions   = map[int64]*deleteChainSession{}
	deleteChainSessionMux sync.Mutex
)

func (c *Config) startTgCommandListener() {
	if !c.Telegram.Enabled || c.Telegram.ApiKey == "" {
		return
	}

	bot, err := tgbotapi.NewBotAPI(c.Telegram.ApiKey)
	if err != nil {
		l("tg command listener:", err)
		return
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	send := func(chatID int64, text string) {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = "Markdown"
		bot.Send(msg)
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}
		chatID := update.Message.Chat.ID

		// handle ongoing sessions (non-command replies)
		addChainSessionMux.Lock()
		session, inAddSession := addChainSessions[chatID]
		addChainSessionMux.Unlock()

		deleteChainSessionMux.Lock()
		delSession, inDelSession := deleteChainSessions[chatID]
		deleteChainSessionMux.Unlock()

		if inAddSession && !update.Message.IsCommand() {
			c.handleAddChainStep(chatID, update.Message.Text, session, send)
			continue
		}
		if inDelSession && !update.Message.IsCommand() {
			c.handleDeleteChainConfirm(chatID, update.Message.Text, delSession, send)
			continue
		}

		if !update.Message.IsCommand() {
			continue
		}

		switch update.Message.Command() {
		case "status":
			arg := strings.TrimSpace(update.Message.CommandArguments())
			send(chatID, c.handleStatusCommand(arg))
		case "list":
			send(chatID, c.handleListCommand())
		case "proposals":
			arg := strings.TrimSpace(update.Message.CommandArguments())
			send(chatID, "🔍 Checking proposals...")
			send(chatID, c.handleProposalsCommand(arg))
		case "addchain":
			addChainSessionMux.Lock()
			addChainSessions[chatID] = &addChainSession{step: 0}
			addChainSessionMux.Unlock()
			send(chatID, "➕ *Add Chain*\n\nStep 1/5: Masukkan nama chain:\n(contoh: `AtomOne Testnet`)")
		case "upgrade":
			arg := strings.TrimSpace(update.Message.CommandArguments())
			send(chatID, "🔍 Checking upgrade plan...")
			send(chatID, c.handleUpgradeCommand(arg))
		case "deletechain":
			arg := strings.TrimSpace(update.Message.CommandArguments())
			if arg == "" {
				send(chatID, c.handleDeleteChainList())
			} else {
				found, msg := c.startDeleteChain(chatID, arg)
				send(chatID, msg)
				if !found {
					break
				}
			}
		case "cancel":
			addChainSessionMux.Lock()
			delete(addChainSessions, chatID)
			addChainSessionMux.Unlock()
			deleteChainSessionMux.Lock()
			delete(deleteChainSessions, chatID)
			deleteChainSessionMux.Unlock()
			send(chatID, "❌ Dibatalkan.")
		}
	}
}

func (c *Config) handleAddChainStep(chatID int64, text string, s *addChainSession, send func(int64, string)) {
	switch s.step {
	case 0:
		s.name = strings.TrimSpace(text)
		s.step = 1
		send(chatID, fmt.Sprintf("✅ Nama: `%s`\n\nStep 2/5: Masukkan chain ID:\n(contoh: `atomone-testnet-1`)", s.name))
	case 1:
		s.chainID = strings.TrimSpace(text)
		s.step = 2
		send(chatID, fmt.Sprintf("✅ Chain ID: `%s`\n\nStep 3/5: Masukkan valoper address:", s.chainID))
	case 2:
		s.valoper = strings.TrimSpace(text)
		s.step = 3
		send(chatID, fmt.Sprintf("✅ Valoper: `%s`\n\nStep 4/5: Masukkan RPC URL:\n(contoh: `tcp://10.32.0.2:26657`)", s.valoper))
	case 3:
		s.rpcURL = strings.TrimSpace(text)
		s.step = 4
		send(chatID, fmt.Sprintf("✅ RPC: `%s`\n\nStep 5/5: Masukkan LCD URL:\n(contoh: `http://10.32.0.2:1317`)\n\nKetik `-` untuk skip jika tidak ada.", s.rpcURL))
	case 4:
		s.lcdURL = strings.TrimSpace(text)
		if s.lcdURL == "-" {
			s.lcdURL = ""
		}
		s.step = 5
		lcdInfo := s.lcdURL
		if lcdInfo == "" {
			lcdInfo = "(tidak ada)"
		}
		send(chatID, fmt.Sprintf(
			"📋 *Konfirmasi:*\n\nNama: `%s`\nChain ID: `%s`\nValoper: `%s`\nRPC: `%s`\nLCD: `%s`\n\nKirim `ya` untuk simpan atau `tidak` untuk batal.",
			s.name, s.chainID, s.valoper, s.rpcURL, lcdInfo,
		))
	case 5:
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "ya", "yes", "y":
			err := c.appendChainToConfig(s)
			addChainSessionMux.Lock()
			delete(addChainSessions, chatID)
			addChainSessionMux.Unlock()
			if err != nil {
				send(chatID, "❌ Gagal menyimpan: "+err.Error())
				return
			}
			send(chatID, fmt.Sprintf("✅ Chain *%s* berhasil ditambahkan!\n\nContainer akan restart dalam 3 detik...", s.name))
			time.Sleep(3 * time.Second)
			os.Exit(0) // Docker akan restart otomatis
		default:
			addChainSessionMux.Lock()
			delete(addChainSessions, chatID)
			addChainSessionMux.Unlock()
			send(chatID, "❌ Dibatalkan.")
		}
	}
}

func (c *Config) appendChainToConfig(s *addChainSession) error {
	if c.configFile == "" {
		c.configFile = "config.yml"
	}
	f, err := os.OpenFile(c.configFile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	lcdLine := ""
	if s.lcdURL != "" {
		lcdLine = fmt.Sprintf("\n    lcd_url: %q", s.lcdURL)
	}

	block := fmt.Sprintf(`
  %q:
    chain_id: %q%s
    valoper_address: %q
    public_fallback: no`, s.name, s.chainID, lcdLine, s.valoper) + `

    alerts:
      stalled_enabled: yes
      stalled_minutes: 10
      consecutive_enabled: yes
      consecutive_missed: 5
      consecutive_priority: critical
      percentage_enabled: yes
      percentage_missed: 10
      percentage_priority: warning
      alert_if_inactive: yes
      alert_if_no_servers: yes
      telegram:
        enabled: yes
        api_key: ""
        channel: ""

    nodes:
      - url: ` + s.rpcURL + `
        alert_if_down: yes
`
	_, err = f.WriteString(block)
	return err
}

func (c *Config) handleUpgradeCommand(chainName string) string {
	c.chainsMux.RLock()
	type target struct {
		name     string
		lcdUrl   string
		blockNum int64
	}
	var targets []target
	if chainName == "" {
		for name, cc := range c.Chains {
			if cc.LcdUrl != "" {
				targets = append(targets, target{name, cc.LcdUrl, cc.lastBlockNum})
			}
		}
		if len(targets) == 0 {
			c.chainsMux.RUnlock()
			return "No chains have lcd_url configured."
		}
	} else {
		for name, cc := range c.Chains {
			if strings.EqualFold(name, chainName) {
				if cc.LcdUrl == "" {
					c.chainsMux.RUnlock()
					return fmt.Sprintf("Chain *%s* has no lcd_url configured.", name)
				}
				targets = append(targets, target{name, cc.LcdUrl, cc.lastBlockNum})
				break
			}
		}
		if len(targets) == 0 {
			c.chainsMux.RUnlock()
			return fmt.Sprintf("Chain '%s' not found.", chainName)
		}
	}
	c.chainsMux.RUnlock()

	var lines []string
	for _, t := range targets {
		plan, err := fetchUpgradePlan(t.lcdUrl)
		if err != nil {
			lines = append(lines, fmt.Sprintf("❌ *%s*: %s", t.name, err.Error()))
			continue
		}
		lines = append(lines, upgradeStatusText(t.name, plan, t.blockNum))
	}
	return strings.Join(lines, "\n\n")
}

func (c *Config) handleProposalsCommand(chainName string) string {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	type target struct {
		name   string
		lcdUrl string
	}
	var targets []target

	if chainName == "" {
		for name, cc := range c.Chains {
			if cc.LcdUrl != "" {
				targets = append(targets, target{name, cc.LcdUrl})
			}
		}
		if len(targets) == 0 {
			return "No chains have lcd_url configured."
		}
	} else {
		for name, cc := range c.Chains {
			if strings.EqualFold(name, chainName) {
				if cc.LcdUrl == "" {
					return fmt.Sprintf("Chain *%s* has no lcd_url configured.", name)
				}
				targets = append(targets, target{name, cc.LcdUrl})
				break
			}
		}
		if len(targets) == 0 {
			return fmt.Sprintf("Chain '%s' not found.", chainName)
		}
	}

	var lines []string
	for _, t := range targets {
		proposals, err := fetchVotingProposals(t.lcdUrl)
		if err != nil {
			lines = append(lines, fmt.Sprintf("❌ *%s*: %s", t.name, err.Error()))
			continue
		}
		if len(proposals) == 0 {
			lines = append(lines, fmt.Sprintf("✅ *%s*: no active proposals", t.name))
			continue
		}
		lines = append(lines, fmt.Sprintf("🗳️ *%s* — %d proposal(s) in voting:", t.name, len(proposals)))
		for _, p := range proposals {
			lines = append(lines, fmt.Sprintf("  📋 #%s: %s\n  ⏰ Ends: %s", p.ID, p.Title, p.EndTime))
		}
	}
	return strings.Join(lines, "\n\n")
}

func (c *Config) handleListCommand() string {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	if len(c.Chains) == 0 {
		return "No chains configured."
	}

	lines := []string{"📋 *Chains monitored:*\n"}
	for name, cc := range c.Chains {
		nodeStatus := fmt.Sprintf("%d/%d nodes", len(cc.Nodes)-countDownNodes(cc), len(cc.Nodes))
		valStatus := "⏳"
		if cc.valInfo != nil && cc.valInfo.Moniker != "not connected" {
			if cc.valInfo.Tombstoned {
				valStatus = "☠️"
			} else if cc.valInfo.Jailed {
				valStatus = "🔴"
			} else if cc.valInfo.Bonded {
				valStatus = "🟢"
			} else {
				valStatus = "⚪"
			}
		}
		lines = append(lines, fmt.Sprintf("%s *%s* — %s", valStatus, name, nodeStatus))
	}
	return strings.Join(lines, "\n")
}

func countDownNodes(cc *ChainConfig) int {
	n := 0
	for _, node := range cc.Nodes {
		if node.down {
			n++
		}
	}
	return n
}

// ── Delete chain ─────────────────────────────────────────────────────────

func (c *Config) handleDeleteChainList() string {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()
	if len(c.Chains) == 0 {
		return "No chains configured."
	}
	lines := []string{"🗑 *Delete Chain*\n\nUsage: `/deletechain <nama>`\n\nChains yang ada:"}
	for name := range c.Chains {
		lines = append(lines, "  • `"+name+"`")
	}
	return strings.Join(lines, "\n")
}

func (c *Config) startDeleteChain(chatID int64, chainName string) (found bool, msg string) {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	var foundName string
	for name := range c.Chains {
		if strings.EqualFold(name, chainName) {
			foundName = name
			break
		}
	}
	if foundName == "" {
		var names []string
		for name := range c.Chains {
			names = append(names, "`"+name+"`")
		}
		return false, fmt.Sprintf("❌ Chain '%s' tidak ditemukan.\n\nYang ada: %s", chainName, strings.Join(names, ", "))
	}

	deleteChainSessionMux.Lock()
	deleteChainSessions[chatID] = &deleteChainSession{chainName: foundName}
	deleteChainSessionMux.Unlock()

	return true, fmt.Sprintf(
		"🗑 *Hapus Chain*\n\nKamu yakin ingin menghapus:\n`%s`\n\n⚠️ Chain akan dihapus dari config dan service akan restart.\n\nKetik `ya` untuk konfirmasi atau `tidak` untuk batal.",
		foundName,
	)
}

func (c *Config) handleDeleteChainConfirm(chatID int64, text string, s *deleteChainSession, send func(int64, string)) {
	deleteChainSessionMux.Lock()
	delete(deleteChainSessions, chatID)
	deleteChainSessionMux.Unlock()

	switch strings.ToLower(strings.TrimSpace(text)) {
	case "ya", "yes", "y":
		err := c.removeChainFromConfig(s.chainName)
		if err != nil {
			send(chatID, "❌ Gagal menghapus: "+err.Error())
			return
		}
		send(chatID, fmt.Sprintf("✅ Chain *%s* berhasil dihapus!\n\nContainer akan restart dalam 3 detik...", s.chainName))
		time.Sleep(3 * time.Second)
		os.Exit(0)
	default:
		send(chatID, "❌ Dibatalkan.")
	}
}

func (c *Config) removeChainFromConfig(chainName string) error {
	if c.configFile == "" {
		c.configFile = "config.yml"
	}
	data, err := os.ReadFile(c.configFile)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")

	// Chain header format:   "Chain Name":
	header := fmt.Sprintf("  %q:", chainName)

	startIdx := -1
	for i, line := range lines {
		if line == header {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		return fmt.Errorf("chain %q tidak ditemukan di config file", chainName)
	}

	// Find end: next line that looks like another chain header (2 spaces + quote)
	endIdx := len(lines)
	for i := startIdx + 1; i < len(lines); i++ {
		if len(lines[i]) >= 3 && lines[i][:2] == "  " && (lines[i][2] == '"' || lines[i][2] == '\'') {
			endIdx = i
			break
		}
	}

	// Also trim blank lines immediately before startIdx
	trimStart := startIdx
	for trimStart > 0 && strings.TrimSpace(lines[trimStart-1]) == "" {
		trimStart--
	}

	result := append(lines[:trimStart], lines[endIdx:]...)
	return os.WriteFile(c.configFile, []byte(strings.Join(result, "\n")), 0600)
}

func (c *Config) handleStatusCommand(chainName string) string {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	if chainName == "" {
		var names []string
		for name := range c.Chains {
			names = append(names, name)
		}
		return "Usage: /status <chain>\nAvailable: " + strings.Join(names, ", ")
	}

	var cc *ChainConfig
	var foundName string
	for name, chain := range c.Chains {
		if strings.EqualFold(name, chainName) {
			cc = chain
			foundName = name
			break
		}
	}

	if cc == nil {
		var names []string
		for name := range c.Chains {
			names = append(names, name)
		}
		return fmt.Sprintf("Chain '%s' not found.\nAvailable: %s", chainName, strings.Join(names, ", "))
	}

	if cc.valInfo == nil || cc.valInfo.Moniker == "not connected" {
		return fmt.Sprintf("⏳ *%s*: not connected yet", foundName)
	}

	status := "🟢 Bonded"
	if cc.valInfo.Tombstoned {
		status = "☠️ Tombstoned"
	} else if cc.valInfo.Jailed {
		status = "🔴 Jailed"
	} else if !cc.valInfo.Bonded {
		status = "⚪ Not Active"
	}

	uptime := "error"
	if cc.valInfo.Window > 0 {
		if cc.valInfo.Missed == 0 {
			uptime = "100%"
		} else {
			uptime = fmt.Sprintf("%.2f%%", 100-float64(cc.valInfo.Missed)/float64(cc.valInfo.Window)*100)
		}
	}

	healthyNodes := 0
	for _, node := range cc.Nodes {
		if !node.down {
			healthyNodes++
		}
	}

	lastBlock := "unknown"
	if !cc.lastBlockTime.IsZero() {
		ago := time.Since(cc.lastBlockTime).Round(time.Second)
		lastBlock = fmt.Sprintf("%d (%s ago)", cc.lastBlockNum, ago)
	}

	return fmt.Sprintf(
		"📊 *%s* (`%s`)\n\n"+
			"Moniker: `%s`\n"+
			"Status: %s\n"+
			"Uptime: %s (%d/%d missed)\n"+
			"Consecutive missed: %d\n"+
			"Last block: %s\n"+
			"Nodes: %d/%d healthy",
		foundName, cc.ChainId,
		cc.valInfo.Moniker,
		status,
		uptime, cc.valInfo.Missed, cc.valInfo.Window,
		int(cc.statConsecutiveMiss),
		lastBlock,
		healthyNodes, len(cc.Nodes),
	)
}
