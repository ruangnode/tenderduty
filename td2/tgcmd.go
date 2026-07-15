package tenderduty

import (
	"fmt"
	"os"
	"sort"
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

func isTestnetChain(name string) bool {
	return strings.Contains(strings.ToLower(name), "testnet")
}

// chainOneLiner returns a compact single-line status for grouped views.
func (c *Config) chainOneLiner(name string, cc *ChainConfig) string {
	emoji, statusStr := "⏳", "connecting"
	uptime := "—"
	healthy := 0
	for _, n := range cc.Nodes {
		if !n.down {
			healthy++
		}
	}
	nodes := fmt.Sprintf("%d/%d", healthy, len(cc.Nodes))

	if cc.valInfo != nil {
		switch {
		case cc.valInfo.Tombstoned:
			emoji, statusStr = "☠️", "Tombstoned"
		case cc.valInfo.Jailed:
			emoji, statusStr = "🔴", "Jailed"
		case cc.valInfo.Bonded:
			emoji, statusStr = "🟢", "Bonded"
		default:
			emoji, statusStr = "⚪", "Inactive"
		}
		if cc.valInfo.Window > 0 {
			if cc.valInfo.Missed == 0 {
				uptime = "100%"
			} else {
				uptime = fmt.Sprintf("%.2f%%", 100-float64(cc.valInfo.Missed)/float64(cc.valInfo.Window)*100)
			}
		}
	}
	return fmt.Sprintf("%s *%s* — %s | ⏱ %s | 🖥 %s", emoji, name, statusStr, uptime, nodes)
}

// chainDetailBlock returns a multi-line detailed status for daily reports.
func (c *Config) chainDetailBlock(name string, cc *ChainConfig) string {
	if cc.valInfo == nil {
		return fmt.Sprintf("⏳ *%s*\n  └ Connecting...\n", name)
	}
	emoji, statusStr := "🟢", "Bonded ✓"
	switch {
	case cc.valInfo.Tombstoned:
		emoji, statusStr = "☠️", "TOMBSTONED"
	case cc.valInfo.Jailed:
		emoji, statusStr = "🔴", "JAILED"
	case !cc.valInfo.Bonded:
		emoji, statusStr = "⚪", "Inactive"
	}

	uptime := "—"
	if cc.valInfo.Window > 0 {
		if cc.valInfo.Missed == 0 {
			uptime = "100%"
		} else {
			uptime = fmt.Sprintf("%.2f%%", 100-float64(cc.valInfo.Missed)/float64(cc.valInfo.Window)*100)
		}
	}

	healthy := 0
	for _, n := range cc.Nodes {
		if !n.down {
			healthy++
		}
	}
	nodeIcon := "✓"
	if healthy < len(cc.Nodes) {
		nodeIcon = "⚠️"
	}

	blockInfo := "—"
	if !cc.lastBlockTime.IsZero() {
		ago := time.Since(cc.lastBlockTime).Round(time.Second)
		blockInfo = fmt.Sprintf("#%d (%s ago)", cc.lastBlockNum, ago)
	}

	return fmt.Sprintf(
		"%s *%s* (`%s`)\n"+
			"  └ Validator: `%s`\n"+
			"  └ Status: %s\n"+
			"  └ Uptime: %s (%d/%d missed)\n"+
			"  └ Block: %s\n"+
			"  └ Nodes: %d/%d %s\n",
		emoji, name, cc.ChainId,
		cc.valInfo.Moniker,
		statusStr,
		uptime, cc.valInfo.Missed, cc.valInfo.Window,
		blockInfo,
		healthy, len(cc.Nodes), nodeIcon,
	)
}

func (c *Config) handleStatusCommand(arg string) string {
	lower := strings.ToLower(strings.TrimSpace(arg))

	// keywords: all, mainnet, testnet
	switch lower {
	case "", "all":
		return c.buildStatusGroup(func(string) bool { return true }, "📊 *All Chains Status*")
	case "mainnet":
		return c.buildStatusGroup(func(name string) bool { return !isTestnetChain(name) }, "🌐 *Mainnet Status*")
	case "testnet":
		return c.buildStatusGroup(isTestnetChain, "🧪 *Testnet Status*")
	}

	// specific chain lookup
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	var cc *ChainConfig
	var foundName string
	for name, chain := range c.Chains {
		if strings.EqualFold(name, arg) {
			cc = chain
			foundName = name
			break
		}
	}
	if cc == nil {
		var names []string
		for name := range c.Chains {
			names = append(names, "`"+name+"`")
		}
		sort.Strings(names)
		return fmt.Sprintf("❌ Chain '%s' tidak ditemukan.\n\nYang tersedia:\n%s\n\nAtau gunakan:\n`/status all` • `/status mainnet` • `/status testnet`", arg, strings.Join(names, "\n"))
	}
	if cc.valInfo == nil {
		return fmt.Sprintf("⏳ *%s*: belum terhubung", foundName)
	}

	emoji, statusStr := "🟢", "Bonded"
	switch {
	case cc.valInfo.Tombstoned:
		emoji, statusStr = "☠️", "Tombstoned"
	case cc.valInfo.Jailed:
		emoji, statusStr = "🔴", "Jailed"
	case !cc.valInfo.Bonded:
		emoji, statusStr = "⚪", "Inactive"
	}

	uptime := "error"
	if cc.valInfo.Window > 0 {
		if cc.valInfo.Missed == 0 {
			uptime = "100%"
		} else {
			uptime = fmt.Sprintf("%.2f%%", 100-float64(cc.valInfo.Missed)/float64(cc.valInfo.Window)*100)
		}
	}
	healthy := len(cc.Nodes) - countDownNodes(cc)
	blockInfo := "unknown"
	if !cc.lastBlockTime.IsZero() {
		ago := time.Since(cc.lastBlockTime).Round(time.Second)
		blockInfo = fmt.Sprintf("%d (%s ago)", cc.lastBlockNum, ago)
	}

	return fmt.Sprintf(
		"%s *%s* (`%s`)\n\n"+
			"Validator: `%s`\n"+
			"Status: %s\n"+
			"Uptime: %s (%d/%d missed)\n"+
			"Consecutive missed: %d\n"+
			"Last block: %s\n"+
			"Nodes: %d/%d healthy",
		emoji, foundName, cc.ChainId,
		cc.valInfo.Moniker,
		statusStr,
		uptime, cc.valInfo.Missed, cc.valInfo.Window,
		int(cc.statConsecutiveMiss),
		blockInfo,
		healthy, len(cc.Nodes),
	)
}

func (c *Config) buildStatusGroup(filterFn func(string) bool, title string) string {
	c.chainsMux.RLock()
	defer c.chainsMux.RUnlock()

	type entry struct{ name string; cc *ChainConfig }
	var mainnets, testnets []entry
	for name, cc := range c.Chains {
		if !filterFn(name) {
			continue
		}
		e := entry{name, cc}
		if isTestnetChain(name) {
			testnets = append(testnets, e)
		} else {
			mainnets = append(mainnets, e)
		}
	}
	sort.Slice(mainnets, func(i, j int) bool { return mainnets[i].name < mainnets[j].name })
	sort.Slice(testnets, func(i, j int) bool { return testnets[i].name < testnets[j].name })

	wib := time.FixedZone("WIB", 7*3600)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n_%s_\n", title, time.Now().In(wib).Format("02 Jan 2006 • 15:04 WIB")))

	if len(mainnets) > 0 {
		bonded := 0
		for _, e := range mainnets {
			if e.cc.valInfo != nil && e.cc.valInfo.Bonded && !e.cc.valInfo.Jailed && !e.cc.valInfo.Tombstoned {
				bonded++
			}
		}
		sb.WriteString(fmt.Sprintf("\n🌐 *Mainnet* — %d/%d Bonded\n", bonded, len(mainnets)))
		for _, e := range mainnets {
			sb.WriteString(c.chainOneLiner(e.name, e.cc) + "\n")
		}
	}
	if len(testnets) > 0 {
		bonded := 0
		for _, e := range testnets {
			if e.cc.valInfo != nil && e.cc.valInfo.Bonded && !e.cc.valInfo.Jailed && !e.cc.valInfo.Tombstoned {
				bonded++
			}
		}
		sb.WriteString(fmt.Sprintf("\n🧪 *Testnet* — %d/%d Bonded\n", bonded, len(testnets)))
		for _, e := range testnets {
			sb.WriteString(c.chainOneLiner(e.name, e.cc) + "\n")
		}
	}
	if len(mainnets)+len(testnets) == 0 {
		sb.WriteString("\n_Tidak ada chain yang sesuai._")
	}
	return sb.String()
}

// ── Daily report ──────────────────────────────────────────────────────────────

func (c *Config) startDailyReport() {
	if !c.Telegram.Enabled || c.Telegram.ApiKey == "" || c.Telegram.Channel == "" {
		return
	}
	wib := time.FixedZone("WIB", 7*3600)
	nextTime := func() time.Duration {
		now := time.Now().In(wib)
		target := time.Date(now.Year(), now.Month(), now.Day(), 7, 0, 0, 0, wib)
		if !now.Before(target) {
			target = target.Add(24 * time.Hour)
		}
		return time.Until(target)
	}

	l(fmt.Sprintf("daily report scheduled, next in %s", nextTime().Round(time.Minute)))
	timer := time.NewTimer(nextTime())
	for {
		<-timer.C
		for _, msg := range c.buildDailyReport() {
			sendTgMessage(c.Telegram.ApiKey, c.Telegram.Channel, msg)
			time.Sleep(300 * time.Millisecond)
		}
		timer.Reset(nextTime())
	}
}

func (c *Config) buildDailyReport() []string {
	wib := time.FixedZone("WIB", 7*3600)
	now := time.Now().In(wib)

	days := []string{"Minggu", "Senin", "Selasa", "Rabu", "Kamis", "Jumat", "Sabtu"}
	months := []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}
	dateStr := fmt.Sprintf("%s, %d %s %d | %s WIB",
		days[now.Weekday()], now.Day(), months[now.Month()-1], now.Year(), now.Format("15:04"))

	// ── Section 1: Status ──────────────────────────────────────────────────
	c.chainsMux.RLock()
	type entry struct{ name string; cc *ChainConfig }
	var mainnets, testnets []entry
	for name, cc := range c.Chains {
		e := entry{name, cc}
		if isTestnetChain(name) {
			testnets = append(testnets, e)
		} else {
			mainnets = append(mainnets, e)
		}
	}
	sort.Slice(mainnets, func(i, j int) bool { return mainnets[i].name < mainnets[j].name })
	sort.Slice(testnets, func(i, j int) bool { return testnets[i].name < testnets[j].name })

	// collect lcd urls for proposals/upgrades (outside lock)
	type lcdEntry struct{ name, lcdUrl string; blockNum int64 }
	var lcdTargets []lcdEntry
	for name, cc := range c.Chains {
		if cc.LcdUrl != "" {
			lcdTargets = append(lcdTargets, lcdEntry{name, cc.LcdUrl, cc.lastBlockNum})
		}
	}
	c.chainsMux.RUnlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("🌅 *Daily Report — RuangNode Monitor*\n📅 %s\n", dateStr))

	if len(mainnets) > 0 {
		bonded := 0
		for _, e := range mainnets {
			if e.cc.valInfo != nil && e.cc.valInfo.Bonded && !e.cc.valInfo.Jailed && !e.cc.valInfo.Tombstoned {
				bonded++
			}
		}
		sb.WriteString(fmt.Sprintf("\n`━━━━━━ 🌐 MAINNET (%d/%d) ━━━━━━`\n\n", bonded, len(mainnets)))
		for _, e := range mainnets {
			sb.WriteString(c.chainDetailBlock(e.name, e.cc))
		}
	}
	if len(testnets) > 0 {
		bonded := 0
		for _, e := range testnets {
			if e.cc.valInfo != nil && e.cc.valInfo.Bonded && !e.cc.valInfo.Jailed && !e.cc.valInfo.Tombstoned {
				bonded++
			}
		}
		sb.WriteString(fmt.Sprintf("\n`━━━━━━ 🧪 TESTNET (%d/%d) ━━━━━━`\n\n", bonded, len(testnets)))
		for _, e := range testnets {
			sb.WriteString(c.chainDetailBlock(e.name, e.cc))
		}
	}
	msg1 := sb.String()

	// ── Section 2: Proposals ──────────────────────────────────────────────
	var sb2 strings.Builder
	sb2.WriteString("`━━━━━━ 🗳️ PROPOSALS ━━━━━━`\n\n")
	anyProp := false
	sort.Slice(lcdTargets, func(i, j int) bool { return lcdTargets[i].name < lcdTargets[j].name })
	for _, t := range lcdTargets {
		proposals, err := fetchVotingProposals(t.lcdUrl)
		if err != nil || len(proposals) == 0 {
			continue
		}
		anyProp = true
		sb2.WriteString(fmt.Sprintf("🗳️ *%s* — %d active:\n", t.name, len(proposals)))
		for _, p := range proposals {
			sb2.WriteString(fmt.Sprintf("  📋 #%s: %s\n  ⏰ Ends: %s\n", p.ID, p.Title, p.EndTime))
		}
		sb2.WriteString("\n")
	}
	if !anyProp {
		sb2.WriteString("✅ Tidak ada proposal aktif\n")
	}

	// ── Section 3: Upgrades ───────────────────────────────────────────────
	sb2.WriteString("\n`━━━━━━ ⬆️ UPGRADES ━━━━━━`\n\n")
	anyUpgrade := false
	for _, t := range lcdTargets {
		plan, err := fetchUpgradePlan(t.lcdUrl)
		if err != nil || plan == nil {
			continue
		}
		anyUpgrade = true
		blocksLeft := plan.Height - t.blockNum
		sb2.WriteString(fmt.Sprintf("⬆️ *%s*: `%s`\n  Block: %d (%d blocks lagi)\n\n",
			t.name, plan.Name, plan.Height, blocksLeft))
	}
	if !anyUpgrade {
		sb2.WriteString("✅ Tidak ada upgrade pending\n")
	}

	return []string{msg1, sb2.String()}
}
