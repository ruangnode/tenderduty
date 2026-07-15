
async function loadState() {
    const enableLogs = await fetch("logsenabled", {
        method: 'GET', mode: 'cors', cache: 'no-cache',
        credentials: 'same-origin', redirect: 'error', referrerPolicy: 'no-referrer'
    });
    let showLog
    try { showLog = await enableLogs.json() } catch(e) { console.log(e) }
    if (showLog.enabled === false) {
        document.getElementById("logContainer").hidden = true
    }

    const response = await fetch("state", {
        method: 'GET', mode: 'cors', cache: 'no-cache',
        credentials: 'same-origin', redirect: 'error', referrerPolicy: 'no-referrer'
    });
    let initialState
    try { initialState = await response.json() } catch(e) { console.log(e) }
    console.log('[RN] /state response:', initialState)
    applyUpdate(initialState)

    const logResponse = await fetch("logs", {
        method: 'GET', mode: 'cors', cache: 'no-cache',
        credentials: 'same-origin', redirect: 'error', referrerPolicy: 'no-referrer'
    });
    let logData
    try { logData = await logResponse.json() } catch(e) { console.log(e) }
    for (let i = logData.length - 1; i >= 0; i--) {
        if (logData[i].ts === 0) { rawLogs.push(""); continue }
        rawLogs.push(`${new Date(logData[i].ts*1000).toLocaleTimeString()} - ${logData[i].msg}`)
    }
    updateLogDisplay()
}

// ── Filter ──────────────────────────────────────────────────────────────
let currentFilter = null
let lastStatus    = null

function isTestnet(name) {
    return name.toLowerCase().includes('testnet')
}

function setFilter(f) {
    document.querySelectorAll('.rn-filter').forEach(b => b.classList.remove('active'))
    if (f === 'all' || currentFilter === f) {
        currentFilter = null
        document.getElementById('filter-all').classList.add('active')
    } else {
        currentFilter = f
        document.getElementById('filter-' + f).classList.add('active')
    }
    if (lastStatus) applyUpdate(lastStatus)
    forceLogUpdate()
}

function forceLogUpdate() {
    const kw = filterKeywords()
    let lines
    if (!currentFilter) {
        lines = [...rawLogs].reverse()
    } else if (kw.length === 0) {
        lines = []
    } else {
        lines = [...rawLogs].reverse().filter(line =>
            !line || kw.some(k => line.toLowerCase().includes(k))
        )
    }
    document.getElementById("logs").innerText = lines.join("\n")
    const el = document.getElementById('rn-log-count')
    if (el) el.innerText = lines.filter(l => l).length + ' entries'
}

document.addEventListener('visibilitychange', function() {
    if (document.visibilityState !== 'hidden') forceLogUpdate()
})

function filteredStatus(status) {
    if (!currentFilter) return status
    return Object.assign({}, status, {
        Status: status.Status.filter(s =>
            currentFilter === 'testnet' ? isTestnet(s.name) : !isTestnet(s.name)
        )
    })
}

function filterKeywords() {
    if (!lastStatus || !currentFilter) return []
    const kw = []
    for (const s of lastStatus.Status) {
        const match = currentFilter === 'testnet' ? isTestnet(s.name) : !isTestnet(s.name)
        if (match) { kw.push(s.name.toLowerCase(), s.chain_id.toLowerCase()) }
    }
    return kw
}

function applyUpdate(status) {
    lastStatus = status
    const fs = filteredStatus(status)
    updateTable(fs)
    drawSeries(fs)   // no-op now; kept for compatibility
    updateLogDisplay()
}

// ── Stats ────────────────────────────────────────────────────────────────
function updateStats(status) {
    const s      = status.Status
    const total  = s.length
    const bonded = s.filter(x => x.bonded && !x.jailed && !x.tombstoned).length
    const jailed = s.filter(x => x.jailed || x.tombstoned).length
    const alerts = s.reduce((n, x) => n + (x.active_alerts || 0), 0)

    const el = id => document.getElementById(id)
    if (el('stat-total'))  el('stat-total').innerText  = total
    if (el('stat-bonded')) el('stat-bonded').innerText = bonded
    if (el('stat-jailed')) el('stat-jailed').innerText = jailed
    if (el('stat-alerts')) el('stat-alerts').innerText = alerts

    const ce = document.getElementById('rn-chain-count')
    if (ce) ce.innerText = total + ' chain' + (total !== 1 ? 's' : '')
}

// ── Table + inline block history ──────────────────────────────────────────
const heightCache = new Map()

// Calculate how many bars fit based on table width
function barSlots() {
    const table = document.querySelector('.rn-table')
    const w = table ? table.offsetWidth : window.innerWidth
    // subtract label col (~120px) + padding + alert col (~40px)
    return Math.max(40, Math.floor((w - 180) / 6))
}

function updateTable(status) {
    if (!status || !status.Status) return
    const tbody = document.getElementById("statusTable")
    updateStats(status)

    const slots = barSlots()
    let html = ''

    for (const s of status.Status) {
        // ── Alert cell ────────────────────────────────────────────────
        let alerts = '&nbsp;'
        if (s.active_alerts > 0 || s.last_error !== '') {
            if (s.last_error !== '') {
                alerts = `
                <a href="#modal-${_.escape(s.name)}" uk-toggle>
                  <span uk-icon="warning" class="rn-alert-icon" uk-tooltip="${_.escape(s.active_alerts)} active issues"></span>
                </a>
                <div id="modal-${_.escape(s.name)}" class="uk-flex-top" uk-modal>
                  <div class="uk-modal-dialog uk-modal-body uk-margin-auto-vertical">
                    <button class="uk-modal-close-default" type="button" uk-close></button>
                    <pre style="font-size:12px;color:var(--dim)">${_.escape(s.last_error)}</pre>
                  </div>
                </div>`
            } else {
                alerts = `<span uk-icon="warning" class="rn-alert-icon" uk-tooltip="${_.escape(s.active_alerts)} active issues"></span>`
            }
        }

        // ── Status badge ───────────────────────────────────────────────
        let bonded, rowStatus
        if (s.tombstoned) {
            bonded = `<span class="rn-badge rn-badge-red">☠ Tombstoned</span>`; rowStatus = 'tombstoned'
        } else if (s.jailed) {
            bonded = `<span class="rn-badge rn-badge-yellow">⚠ Jailed</span>`; rowStatus = 'jailed'
        } else if (s.bonded) {
            bonded = `<span class="rn-badge rn-badge-green">✓ Bonded</span>`; rowStatus = 'bonded'
        } else {
            bonded = `<span class="rn-badge rn-badge-gray">— Inactive</span>`; rowStatus = 'inactive'
        }

        // ── Uptime ─────────────────────────────────────────────────────
        let uptimePct = 0, uptimeStr = '—', barClass = ''
        if (s.missed === 0 && s.window === 0) {
            uptimeStr = 'error'
        } else if (s.missed === 0) {
            uptimePct = 100; uptimeStr = '100%'
        } else {
            uptimePct = 100 - (s.missed / s.window) * 100
            uptimeStr = uptimePct.toFixed(2) + '%'
        }
        if (uptimePct < 95) barClass = 'danger'
        else if (uptimePct < 99) barClass = 'warn'

        const uptimeCell = `
          <div class="rn-uptime-pct">${_.escape(uptimeStr)}</div>
          <div class="rn-uptime-bar"><div class="rn-uptime-fill ${barClass}" style="width:${Math.max(0,uptimePct)}%"></div></div>
          <div class="rn-uptime-detail">${_.escape(s.missed)} / ${_.escape(s.window)}</div>`

        // ── Nodes ──────────────────────────────────────────────────────
        const nodesClass = s.healthy_nodes < s.nodes ? 'rn-nodes rn-nodes-warn' : 'rn-nodes rn-nodes-ok'
        const nodesCell  = `<span class="${nodesClass}">${_.escape(s.healthy_nodes)}/${_.escape(s.nodes)}</span>`

        // ── Height (animate on change) ─────────────────────────────────
        const heightClass = heightCache.get(s.chain_id) !== s.height ? 'uk-animation-scale-up' : ''
        heightCache.set(s.chain_id, s.height)

        // ── Moniker ────────────────────────────────────────────────────
        let monikerHtml
        if (s.moniker === 'not connected') {
            monikerHtml = `<span style="color:var(--yellow)">not connected</span>`
            bonded    = `<span class="rn-badge rn-badge-gray">unknown</span>`
            rowStatus = 'inactive'
        } else {
            monikerHtml = `<span class="rn-moniker">${_.escape(s.moniker.substring(0, 24))}</span>`
        }

        // ── Block signing history bars ─────────────────────────────────
        const barsHtml = buildBlockBars(s.blocks, slots)

        // ── Main row ───────────────────────────────────────────────────
        html += `<tr data-status="${rowStatus}">
            <td><div>${alerts}</div></td>
            <td><div class="rn-chain-name">${_.escape(s.name)}</div><div class="rn-chain-id">${_.escape(s.chain_id)}</div></td>
            <td><div class="rn-height ${heightClass}">${_.escape(s.height)}</div></td>
            <td>${monikerHtml}</td>
            <td>${bonded}</td>
            <td>${uptimeCell}</td>
            <td>${nodesCell}</td>
        </tr>`

        // ── History row ────────────────────────────────────────────────
        html += `<tr class="rn-hist-row" data-status="${rowStatus}">
            <td colspan="7">
                <div class="rn-hist">
                    <span class="rn-hist-label">Block History</span>
                    <div class="rn-hist-bars">${barsHtml}</div>
                </div>
            </td>
        </tr>`
    }

    tbody.innerHTML = html
}

// ── Logs ─────────────────────────────────────────────────────────────────
let rawLogs = []

function addLogMsg(str) {
    if (rawLogs.length >= 256) rawLogs.shift()
    rawLogs.push(str)
    updateLogDisplay()
}

function updateLogDisplay() {
    if (document.visibilityState === 'hidden') return
    let lines
    if (!currentFilter) {
        lines = [...rawLogs].reverse()
    } else {
        const kw = filterKeywords()
        lines = kw.length === 0
            ? []
            : [...rawLogs].reverse().filter(line => !line || kw.some(k => line.toLowerCase().includes(k)))
    }
    document.getElementById('logs').innerText = lines.join('\n')
    const el = document.getElementById('rn-log-count')
    if (el) el.innerText = lines.filter(l => l).length + ' entries'
}

// ── WebSocket ─────────────────────────────────────────────────────────────
function connect() {
    const wsProto = location.protocol === 'https:' ? 'wss://' : 'ws://'
    const parse = function(event) {
        const msg = JSON.parse(event.data)
        if (msg.msgType === 'log') {
            addLogMsg(`${new Date(msg.ts*1000).toLocaleTimeString()} - ${msg.msg}`)
        } else if (msg.msgType === 'update' && document.visibilityState !== 'hidden') {
            applyUpdate(msg)
        }
        event = null
    }
    const socket = new WebSocket(wsProto + location.host + '/ws')
    socket.addEventListener('message', function(event) { parse(event) })
    socket.onclose = function(e) {
        console.log('Socket closed, retrying...', e.reason)
        addLogMsg('Socket closed, retrying /ws... ' + e.reason)
        setTimeout(connect, 3000)
    }
}
connect()
