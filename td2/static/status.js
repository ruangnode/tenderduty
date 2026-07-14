
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
let currentFilter = null  // null = show all
let lastStatus = null

function isTestnet(name) {
    return name.toLowerCase().includes('testnet')
}

function setFilter(f) {
    if (currentFilter === f) {
        currentFilter = null
        document.querySelectorAll('.rn-filter').forEach(b => b.classList.remove('active'))
    } else {
        currentFilter = f
        document.querySelectorAll('.rn-filter').forEach(b => b.classList.remove('active'))
        document.getElementById('filter-' + f).classList.add('active')
    }
    if (lastStatus) applyUpdate(lastStatus)
}

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
    drawSeries(fs)
    updateLogDisplay()
}

// ── Table ────────────────────────────────────────────────────────────────
const blocks = new Map()

function updateTable(status) {
    const tbody = document.getElementById("statusTable")
    for (let i = tbody.rows.length; i > 0; i--) tbody.deleteRow(i - 1)

    for (let i = 0; i < status.Status.length; i++) {
        const s = status.Status[i]

        // Alert cell
        let alerts = "&nbsp;"
        if (s.active_alerts > 0 || s.last_error !== "") {
            if (s.last_error !== "") {
                alerts = `
                <a href="#modal-${_.escape(s.name)}" uk-toggle>
                  <span uk-icon="warning" class="rn-alert-icon" uk-tooltip="${_.escape(s.active_alerts)} active issues"></span>
                </a>
                <div id="modal-${_.escape(s.name)}" class="uk-flex-top" uk-modal>
                  <div class="uk-modal-dialog uk-modal-body uk-margin-auto-vertical" style="background:#151b2e;color:#e2e8f0">
                    <button class="uk-modal-close-default" type="button" uk-close></button>
                    <pre style="color:#94a3b8;font-size:12px">${_.escape(s.last_error)}</pre>
                  </div>
                </div>`
            } else {
                alerts = `<span uk-icon="warning" class="rn-alert-icon" uk-tooltip="${_.escape(s.active_alerts)} active issues"></span>`
            }
        }

        // Status badge
        let bonded
        if (s.tombstoned) {
            bonded = `<span class="rn-badge rn-badge-red">☠ Tombstoned</span>`
        } else if (s.jailed) {
            bonded = `<span class="rn-badge rn-badge-yellow">⚠ Jailed</span>`
        } else if (s.bonded) {
            bonded = `<span class="rn-badge rn-badge-green">✓ Bonded</span>`
        } else {
            bonded = `<span class="rn-badge rn-badge-gray">— Inactive</span>`
        }

        // Uptime
        let uptimePct = 0, uptimeStr = "—", barClass = ""
        if (s.missed === 0 && s.window === 0) {
            uptimeStr = "error"
        } else if (s.missed === 0) {
            uptimePct = 100; uptimeStr = "100%"
        } else {
            uptimePct = 100 - (s.missed / s.window) * 100
            uptimeStr = uptimePct.toFixed(2) + "%"
        }
        if (uptimePct < 95) barClass = "danger"
        else if (uptimePct < 99) barClass = "warn"

        const uptimeCell = `
          <div class="rn-uptime-pct">${_.escape(uptimeStr)}</div>
          <div class="rn-uptime-bar"><div class="rn-uptime-fill ${barClass}" style="width:${Math.max(0,uptimePct)}%"></div></div>
          <div class="rn-uptime-detail">${_.escape(s.missed)} / ${_.escape(s.window)}</div>`

        // Nodes
        const nodesClass = s.healthy_nodes < s.nodes ? "rn-nodes rn-nodes-warn" : "rn-nodes rn-nodes-ok"
        const nodesCell = `<span class="${nodesClass}">${_.escape(s.healthy_nodes)}/${_.escape(s.nodes)}</span>`

        // Height animation
        let heightClass = ""
        if (blocks.get(s.chain_id) !== s.height) heightClass = "uk-animation-scale-up"
        blocks.set(s.chain_id, s.height)

        // Moniker
        let monikerHtml
        if (s.moniker === "not connected") {
            monikerHtml = `<span style="color:#f59e0b">not connected</span>`
            bonded = `<span class="rn-badge rn-badge-gray">unknown</span>`
        } else {
            monikerHtml = `<span class="rn-moniker">${_.escape(s.moniker.substring(0,24))}</span>`
        }

        const r = tbody.insertRow(i)
        r.insertCell(0).innerHTML = `<div>${alerts}</div>`
        r.insertCell(1).innerHTML = `<div class="rn-chain-name">${_.escape(s.name)}</div><div class="rn-chain-id">${_.escape(s.chain_id)}</div>`
        r.insertCell(2).innerHTML = `<div class="rn-height ${heightClass}">${_.escape(s.height)}</div>`
        r.insertCell(3).innerHTML = monikerHtml
        r.insertCell(4).innerHTML = bonded
        r.insertCell(5).innerHTML = uptimeCell
        r.insertCell(6).innerHTML = nodesCell
    }
}

// ── Logs ─────────────────────────────────────────────────────────────────
let rawLogs = []

function addLogMsg(str) {
    if (rawLogs.length >= 256) rawLogs.shift()
    rawLogs.push(str)
    updateLogDisplay()
}

function updateLogDisplay() {
    if (document.visibilityState === "hidden") return
    const kw = filterKeywords()
    let lines
    if (kw.length === 0) {
        lines = [...rawLogs].reverse()
    } else {
        lines = [...rawLogs].reverse().filter(line =>
            !line || kw.some(k => line.toLowerCase().includes(k))
        )
    }
    document.getElementById("logs").innerText = lines.join("\n")
}

// ── WebSocket ─────────────────────────────────────────────────────────────
function connect() {
    let wsProto = location.protocol === "https:" ? "wss://" : "ws://"
    const parse = function(event) {
        const msg = JSON.parse(event.data)
        if (msg.msgType === "log") {
            addLogMsg(`${new Date(msg.ts*1000).toLocaleTimeString()} - ${msg.msg}`)
        } else if (msg.msgType === "update" && document.visibilityState !== "hidden") {
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
