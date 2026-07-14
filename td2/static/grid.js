const h = 24
const w = 9
const textMax = 115
const textW = 120
let gridH = h
let gridW = w
let gridTextMax = textMax
let gridTextW = textW
let scale = 1
let textColor = "#b0b0b0"

let signColorAlpha = 0.4
let isDark = true

// Color palette
const C = {
    proposer:      ['#1d4ed8', '#3b82f6', '#60a5fa'],  // blue
    signed:        ['#15803d', '#22c55e', '#4ade80'],  // green
    precommit:     ['#c2410c', '#f97316', '#fb923c'],  // orange
    prevote:       ['#7e22ce', '#a855f7', '#c084fc'],  // purple
    missed:        ['#991b1b', '#ef4444', '#f87171'],  // red
    nodata:        ['#111827', '#1f2937', '#374151'],  // near-black
}

function lightMode() {
    isDark = !isDark
    if (isDark) {
        textColor = "#b0b0b0"
        signColorAlpha = 0.4
        document.body.classList.remove('light-mode')
        document.body.className = "uk-background-secondary uk-light"
        document.getElementById('canvasDiv').className = "uk-width-expand uk-overflow-auto uk-background-secondary"
        document.getElementById("tableDiv").className = "uk-padding-small uk-text-small uk-background-secondary uk-overflow-auto"
        document.getElementById("legendContainer").className = "uk-nav-center uk-background-secondary uk-padding-remove"
        return
    }
    textColor = "#3f3f3f"
    signColorAlpha = 0.2
    document.body.classList.add('light-mode')
    document.body.className = "uk-background-default uk-text-default light-mode"
    document.getElementById('canvasDiv').className = "uk-width-expand uk-overflow-auto uk-background-default"
    document.getElementById("tableDiv").className = "uk-padding-small uk-text-small uk-background-default uk-overflow-auto"
    document.getElementById("legendContainer").className = "uk-nav-center uk-background-default uk-padding-remove"
}

function fix_dpi(id) {
    let canvas = document.getElementById(id),
        dpi = window.devicePixelRatio;
    gridH = h * dpi.valueOf()
    gridW = w * dpi.valueOf()
    gridTextMax = textMax * dpi.valueOf()
    gridTextW = textW * dpi.valueOf()
    let style = {
        height() { return +getComputedStyle(canvas).getPropertyValue('height').slice(0,-2); },
        width()  { return +getComputedStyle(canvas).getPropertyValue('width').slice(0,-2); }
    }
    canvas.setAttribute('width', style.width() * dpi);
    canvas.setAttribute('height', style.height() * dpi);
    scale = dpi.valueOf()
}

function makeGrad(ctx, offset, colors) {
    const g = ctx.createLinearGradient(offset, 0, offset + gridW, gridH)
    g.addColorStop(0,   colors[0])
    g.addColorStop(0.5, colors[1])
    g.addColorStop(1,   colors[2])
    return g
}

function legend() {
    const l = document.getElementById("legend")
    l.height = scale * h * 1.2
    const ctx = l.getContext('2d')
    ctx.font = `${scale * 13}px sans-serif`

    const items = [
        { colors: C.proposer,  label: "proposer" },
        { colors: C.signed,    label: "signed" },
        { colors: C.precommit, label: "miss/precommit" },
        { colors: C.prevote,   label: "miss/prevote" },
        { colors: C.missed,    label: "missed" },
        { colors: C.nodata,    label: "no data" },
    ]

    let offset = textW
    for (const item of items) {
        ctx.fillStyle = makeGrad(ctx, offset, item.colors)
        ctx.fillRect(offset, 0, gridW, gridH)
        offset += gridW + gridW / 2
        ctx.fillStyle = '#94a3b8'
        ctx.fillText(item.label, offset, gridH / 1.2)
        offset += ctx.measureText(item.label).width + 16 * scale
    }
}

function drawSeries(multiStates) {
    const canvas = document.getElementById("canvas")
    canvas.height = ((12 * gridH * multiStates.Status.length) / 10) + 30
    fix_dpi("canvas")
    if (!canvas.getContext) return

    const ctx = canvas.getContext('2d')
    ctx.font = `${scale * 14}px sans-serif`

    for (let j = 0; j < multiStates.Status.length; j++) {
        ctx.fillStyle = textColor
        ctx.fillText(multiStates.Status[j].name, 5, (j * gridH) + (gridH * 2) - 6, gridTextMax)

        for (let i = 0; i < multiStates.Status[j].blocks.length; i++) {
            let colors
            let crossThrough = false

            switch (multiStates.Status[j].blocks[i]) {
                case 4: colors = C.proposer;  break
                case 3: colors = C.signed;    break
                case 2: colors = C.precommit; break
                case 1: colors = C.prevote;   break
                case 0: colors = C.missed; crossThrough = true; break
                default: colors = C.nodata
            }

            const x = (i * gridW) + gridTextW
            const y = gridH + (gridH * j)

            ctx.clearRect(x, y, gridW, gridH)
            ctx.fillStyle = makeGrad(ctx, x, colors)
            ctx.fillRect(x, y, gridW, gridH)

            // row separator line
            if (i > 0) {
                ctx.beginPath()
                ctx.moveTo(x - gridW, 2 * gridH + (gridH * j) - 0.5)
                ctx.lineTo(x, 2 * gridH + (gridH * j) - 0.5)
                ctx.closePath()
                ctx.strokeStyle = 'rgba(0,0,0,0.4)'
                ctx.stroke()
            }

            // cross line for missed
            if (crossThrough) {
                ctx.beginPath()
                ctx.moveTo(x + 1 + gridW / 4, y + gridH - gridH / 2)
                ctx.lineTo(x + gridW - gridW / 4 - 1, y + gridH - gridH / 2)
                ctx.closePath()
                ctx.strokeStyle = 'rgba(255,255,255,0.8)'
                ctx.lineWidth = 1.5
                ctx.stroke()
            }
        }
    }
}
