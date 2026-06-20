// SVG icons for status
const iconCheck = `<svg class="inline w-5 h-5 text-green-500 mr-1" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>`
const iconCross = `<svg class="inline w-5 h-5 text-red-500 mr-1" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/></svg>`
const iconInfo = `<svg class="inline w-5 h-5 text-blue-500 mr-1" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01"/></svg>`

// Link to the ipfs-check backend repo, surfaced when the configured backend is
// slow or unreachable so users can run their own and point Backend URL at it.
const selfHostedBackendLink = `<a href='https://github.com/ipfs/ipfs-check#self-hosting' target='_blank' rel='noopener noreferrer' class='text-blue-600 hover:text-blue-800 underline'>self-hosted ipfs-check backend</a>`

// Appended to every failure box so the self-hosting option is always one click
// away, whatever the backend problem was.
const selfHostTip = `<span class='block mt-2 text-sm'>Tip: you can run a ${selfHostedBackendLink} and use it as your <b>Backend URL</b>.</span>`

window.addEventListener('load', function () {
    initFormValues(new URL(window.location))
    
    const plausible = window.plausible || function() {
        window.plausible = window.plausible || { q: [] };
        try {
          window.plausible.q.push(arguments);
        } catch (e) {
          // Silent fallback - analytics shouldn't break the app
        }
    }


    const queryForm = document.getElementById('queryForm')
    if (!queryForm) {
        console.error('Query form not found')
        return
    }
    
    let countdownInterval = null
    
    // Clear results when CID field value changes
    const cidInput = document.getElementById('cid')
    if (cidInput) {
        cidInput.addEventListener('change', function() {
            showOutput('') // clear out previous results
            showRawOutput('') // clear out previous results
            // Clearing results returns us to a fresh state, so drop any
            // leftover "Retry" label from a prior timeout. Skip while a check
            // is running (button disabled) to leave the live countdown intact.
            const button = document.getElementById('submit')
            const buttonText = document.getElementById('button-text')
            if (button && buttonText && !button.disabled) {
                buttonText.textContent = 'Run Test'
            }
        })
    }
    
    queryForm.addEventListener('submit', async function (e) {
        e.preventDefault() // dont do a browser form post

        showOutput('') // clear out previous results
        showRawOutput('') // clear out previous results

        const formData = new FormData(queryForm)
        const backendURL = getBackendUrl(formData)
        const inputMaddr = formData.get('multiaddr')

        // Start countdown timer
        const timeoutSeconds = parseInt(formData.get('timeoutSeconds')) || 30
        // The backend bounds its own work by timeoutSeconds. Give it that long
        // plus a small leniency for network and serialization overhead, then
        // abort the request. Without this the fetch hangs indefinitely when the
        // backend stalls past its deadline (e.g. overloaded or unreachable).
        const abortAfterSeconds = timeoutSeconds + 5
        startCountdown(timeoutSeconds)

        plausible('IPFS Check Run', {
            props: {
                withMultiaddr: inputMaddr !== ''
            },
        })

        showInQuery(formData) // add `cid` and `multiaddr` to local url query to make it shareable
        toggleSubmitButton()

        const controller = new AbortController()
        const abortTimer = setTimeout(() => controller.abort(), abortAfterSeconds * 1000)
        let failed = false
        // Set once fetch resolves, to tell connect failures (bad URL, dead
        // domain, CORS, offline) apart from failures reading the response.
        let reached = false
        try {
          const res = await fetch(backendURL, { method: 'POST', signal: controller.signal })
          reached = true

          if (res.ok) {
              const respObj = await res.json()
              showRawOutput(JSON.stringify(respObj, null, 2))

              // Rendering is separate from reading: a formatter throwing on an
              // unexpected (but valid) response shape is a display bug, not a
              // backend failure, so report it honestly and leave the button as
              // Run Test (a retry would fail the same way). The raw JSON above
              // stays available.
              try {
                if(inputMaddr === '') {
                  const output = formatJustCidOutput(respObj)
                  showOutput(output)
                } else {
                  const output = formatMaddrOutput(inputMaddr, respObj)
                  showOutput(output)
                }
              } catch (renderErr) {
                console.log(renderErr)
                showOutput(formatRenderErrorOutput(renderErr))
              }
          } else {
              failed = true
              const resText = await res.text()
              showOutput(formatHttpErrorOutput(backendURL.host, res.status, resText))
          }
        } catch (e) {
          failed = true
          if (e.name === 'AbortError') {
            showOutput(formatTimeoutOutput(timeoutSeconds, abortAfterSeconds, backendURL.host))
          } else {
            console.log(e)
            showOutput(formatRequestErrorOutput(backendURL.host, reached, e))
          }
        } finally {
          clearTimeout(abortTimer)
          // Any failure relabels the button to Retry so another click re-runs
          // the same check (the button is a form submit, so the click re-runs
          // this handler). A successful run restores the default label.
          stopCountdown(failed ? 'Retry' : 'Run Test')
          toggleSubmitButton()
        }
    })
    
    function startCountdown(seconds) {
        // Clear any existing timer without touching the label; it is set below.
        clearCountdownTimer()

        const buttonText = document.getElementById('button-text')
        let remaining = seconds

        // Update button text immediately
        if (buttonText) {
            buttonText.textContent = `Testing: ${remaining}s`
        }

        // Update every second
        countdownInterval = setInterval(() => {
            remaining--
            if (remaining > 0) {
                if (buttonText) {
                    buttonText.textContent = `Testing: ${remaining}s`
                }
            } else {
                // Past the requested timeout. The request is still in flight
                // during the leniency window before it is aborted, so keep the
                // button in a busy state rather than resetting its label.
                clearCountdownTimer()
                if (buttonText) {
                    buttonText.textContent = 'Waiting…'
                }
            }
        }, 1000)
    }

    function clearCountdownTimer() {
        if (countdownInterval) {
            clearInterval(countdownInterval)
            countdownInterval = null
        }
    }

    function stopCountdown(label = 'Run Test') {
        clearCountdownTimer()

        // Restore the button label (default 'Run Test', or 'Retry' on abort).
        const buttonText = document.getElementById('button-text')
        if (buttonText) {
            buttonText.textContent = label
        }
    }
})

function initFormValues (url) {
    for (const [key, val] of url.searchParams) {
        document.getElementById(key)?.setAttribute('value', val)
    }

    const timeoutSlider = document.getElementById('timeoutSeconds')
    const timeoutValue = document.getElementById('timeoutValue')

    if (timeoutSlider && timeoutValue) {
        timeoutSlider.addEventListener('input', function() {
            timeoutValue.textContent = this.value
        })
        // set initial value
        timeoutValue.textContent = timeoutSlider.value
    }
}

function showInQuery (formData) {
    const backendURLElement = document.getElementById('backendURL')
    const defaultBackendUrl = backendURLElement ? backendURLElement.getAttribute('placeholder') : null
    const params = new URLSearchParams(formData)
    // skip showing default value our shareable url
    if (defaultBackendUrl && params.get('backendURL') === defaultBackendUrl) {
        params.delete('backendURL')
    }
    const url = new URL('?' + params, window.location)
    history.replaceState(null, "", url)
}

function getBackendUrl (formData) {
    const params = new URLSearchParams(formData)
    // dont send backendURL to the backend!
    params.delete('backendURL')
    // backendURL is the base, params are appended as query string
    try {
        return new URL('/check?' + params, formData.get('backendURL'))
    } catch (e) {
        console.error('Invalid backend URL:', e)
        // Fallback to current origin
        return new URL('/check?' + params, window.location.origin)
    }
}

function showOutput (output) {
    const outObj = document.getElementById('output')
    if (!outObj) {
        console.error('Output element not found')
        return
    }
    outObj.innerHTML = output
    
    // Show/hide raw output details based on whether there's output
    const rawOutputDetails = document.querySelector('details:has(#raw-output)')
    if (rawOutputDetails) {
        if (output && output.trim()) {
            rawOutputDetails.style.display = 'block'
        } else {
            rawOutputDetails.style.display = 'none'
        }
    }
}

function showRawOutput (output) {
    const outObj = document.getElementById('raw-output')
    if (!outObj) {
        console.error('Raw output element not found')
        return
    }
    outObj.textContent = output
}

function toggleSubmitButton() {
    const button = document.getElementById('submit')
    if (button) {
        button.toggleAttribute('disabled')
    }
    const spinner = document.getElementById('loading-spinner')
    if (spinner) {
        // Toggle spinner visibility
        spinner.classList.toggle('hidden')
    }
}

// htmlEscapes is hoisted so escapeHtml builds the lookup once, not per char.
const htmlEscapes = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }

// escapeHtml makes a backend-supplied string safe to interpolate as HTML text
// or into a quoted attribute. Error bodies and exception messages can carry
// markup (e.g. a JSON parse error quotes the offending "<html>..." page), which
// would otherwise render as HTML.
function escapeHtml (s) {
    return String(s).replace(/[&<>"']/g, (c) => htmlEscapes[c])
}

// codeSnippet renders a backend-supplied value as escaped inline <code>.
function codeSnippet (text) {
    return `<code class='bg-gray-100 px-1 rounded'>${escapeHtml(text)}</code>`
}

// errText extracts a displayable string from a thrown value.
function errText (err) {
    return String((err && err.message) || err || 'unknown error')
}

// backendLabel returns " <code>host</code>" (note the leading space) to drop
// into a "the backend<label>" sentence, or '' when the host is unknown.
function backendLabel (host) {
    return host ? ` ${codeSnippet(host)}` : ''
}

// failureBox renders a styled message box for a failed check, always appending
// the self-hosting tip. warn=true uses the softer yellow style (timeouts);
// other failures use red.
function failureBox (message, warn) {
    const style = warn
        ? 'bg-yellow-100 border-l-4 border-yellow-500 text-yellow-800'
        : 'bg-red-100 border-l-4 border-red-500 text-red-700'
    return `<div class='${style} p-4 rounded mb-4 flex gap-x-2 items-start'>${warn ? iconInfo : iconCross}<span>${message}${selfHostTip}</span></div>`
}

function formatTimeoutOutput (timeoutSeconds, abortAfterSeconds, backendHost) {
    return failureBox(`The backend${backendLabel(backendHost)} did not finish responding within <b>${abortAfterSeconds}s</b> (your ${timeoutSeconds}s timeout plus 5s leniency), so the request was aborted. It may be overloaded or unreachable. Press <b>Retry</b> to run the check again, or raise the <b>Check Timeout</b> in <b>Backend Config</b>.`, true)
}

function formatHttpErrorOutput (backendHost, status, body) {
    const trimmed = (body || '').trim()
    const detail = trimmed ? `: ${codeSnippet(trimmed)}` : ''
    // 4xx is a client error: retrying the same request fails the same way, so
    // steer the user to fix their input. 5xx and the rest are transient.
    const action = status >= 400 && status < 500
        ? 'Check your input, then press <b>Retry</b>.'
        : 'Press <b>Retry</b> to run the check again.'
    return failureBox(`The backend${backendLabel(backendHost)} returned an error (HTTP <b>${status}</b>)${detail}. ${action}`)
}

// formatRenderErrorOutput covers a response that was received and parsed but
// could not be rendered (an unexpected shape made a formatter throw). It is a
// display issue rather than a backend failure, so it points at the raw JSON
// instead of suggesting a retry.
function formatRenderErrorOutput (err) {
    return failureBox(`The check ran, but its response could not be displayed (${codeSnippet(errText(err))}). See <b>Raw Output</b> below for the full response.`)
}

// formatRequestErrorOutput covers a fetch that never produced a response
// (reached=false: bad URL, dead domain, DNS failure, connection refused, CORS,
// TLS, offline) and a response that was received but could not be read
// (reached=true, e.g. the body was not valid JSON).
function formatRequestErrorOutput (backendHost, reached, err) {
    if (!reached) {
        return failureBox(`Could not reach the backend${backendLabel(backendHost)}. The address may be wrong, the server may be down, or it may be blocking cross-origin (CORS) requests. Check the <b>Backend URL</b> in <b>Backend Config</b>, then press <b>Retry</b>.`)
    }
    return failureBox(`The backend${backendLabel(backendHost)} returned a response that could not be read (${codeSnippet(errText(err))}). Press <b>Retry</b> to run the check again.`)
}

function formatMutableResolution(mutableRes) {
    if (!mutableRes) return ''

    // Extract domain from diagnostic URL for display (without hash)
    let diagnosticSite = ''
    if (mutableRes.DiagnosticURL) {
        try {
            const url = new URL(mutableRes.DiagnosticURL)
            diagnosticSite = url.hostname
        } catch (e) {
            diagnosticSite = 'diagnostic tool'
        }
    }

    let html = '<div class="mb-4">'

    // Show warning if input is mutable or legacy format
    if (mutableRes.IsMutableInput) {
        html += `<div class="mb-3"><span class="text-lg font-bold">⚠️ Input is not an immutable CID, assuming mutable pointer</span></div>`
    }

    if (mutableRes.Error) {
        // Resolution failed
        html += `<span class="text-lg font-bold">${iconCross} Mutable pointer resolution failed</span>`
        html += ` <span class="text-gray-600">for <code class="bg-gray-100 px-2 py-1 rounded text-sm">${mutableRes.InputPath || 'unknown'}</code></span>`
        html += `<div class="mt-2 text-sm text-red-700">Error: <code class="bg-red-100 px-2 py-1 rounded">${mutableRes.Error}</code></div>`
        if (mutableRes.DiagnosticURL) {
            html += `<div class="mt-2 text-sm"><a href="${mutableRes.DiagnosticURL}" target="_blank" rel="noopener noreferrer" class="text-blue-600 hover:text-blue-800 underline">More details at ${diagnosticSite}</a></div>`
        }
    } else {
        // Resolution succeeded
        html += `<span class="text-lg font-bold">${iconCheck} Mutable pointer resolved successfully</span>`
        html += '</div>'
        // Add details box similar to provider results
        html += '<div class="rounded-lg shadow bg-white border-gray-200 p-4 border mb-4">'
        html += '<div class="text-sm mb-2"><span class="font-bold">Input:</span> <span class="font-mono text-xs break-all">' + (mutableRes.InputPath || 'unknown') + '</span></div>'
        html += '<div class="text-sm mb-2"><span class="font-bold">Resolved to:</span> <span class="font-mono text-xs break-all">' + (mutableRes.ResolvedPath || 'unknown') + '</span></div>'
        if (mutableRes.DiagnosticURL) {
            html += '<div class="text-sm mt-3"><a href="' + mutableRes.DiagnosticURL + '" target="_blank" rel="noopener noreferrer" class="text-blue-600 hover:text-blue-800 underline">More details at ' + diagnosticSite + '</a></div>'
        }
        html += '</div>'
        html += '<div class="mb-4">'
    }

    html += '</div>'
    return html
}

function formatMaddrOutput (multiaddr, respObj) {
    const peerIDStartIndex = multiaddr.lastIndexOf("/p2p/")
    const peerID = multiaddr.slice(peerIDStartIndex + 5);
    const addrPart = multiaddr.slice(0, peerIDStartIndex);
    let outHtml = `<div class='space-y-4'>`

    // Show resolution info if present (at top level)
    outHtml += formatMutableResolution(respObj.MutableResolution)

    // Extract actual peer check result (might be wrapped)
    const peerResult = respObj.Result || respObj

    // Connection status
    if (peerResult.ConnectionError !== "") {
        outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not connect to multiaddr: <span class='font-mono'>${peerResult.ConnectionError}</span></span></div>`
    } else {
        const madrs = peerResult?.ConnectionMaddrs
        outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Successfully connected to multiaddr${madrs?.length > 1 ? 's' : '' }:<br><span class='font-mono text-xs block ml-6'>${madrs.join('<br>')}</span></span></div>`
    }

    // DHT status
    if (multiaddr.indexOf("/p2p/") === 0 && multiaddr.lastIndexOf("/") === 4) {
        // only peer id passed with /p2p/PeerID
        if (Object.keys(peerResult.PeerFoundInDHT).length === 0) {
            outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not find any multiaddrs in the DHT</span></div>`
        } else {
            outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Found multiaddrs advertised in the DHT:<br><span class='font-mono text-xs block ml-6'>${Object.keys(peerResult.PeerFoundInDHT).join('<br>')}</span></span></div>`
        }
    } else {
        // a proper maddr with an IP was passed
        let foundAddr = false
        for (const key in peerResult.PeerFoundInDHT) {
            if (key === addrPart) {
                foundAddr = true
                outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Found multiaddr with <span class='font-bold'>${peerResult.PeerFoundInDHT[key]}</span> DHT peers</span></div>`
                break
            }
        }
        if (!foundAddr) {
            let alt = ''
            if (Object.keys(peerResult.PeerFoundInDHT).length > 0) {
              alt = `<br>Instead found:<br><span class='font-mono text-xs block ml-6 break-all'>${Object.keys(peerResult.PeerFoundInDHT).join('<br>')}</span>`
            } else {
              alt = '<br>No other addresses were found.'
            }
            outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not find the given multiaddr in the DHT.${alt}</span></div>`
        }
    }

    // Provider record
    if (peerResult.ProviderRecordFromPeerInDHT === true || peerResult.ProviderRecordFromPeerInIPNI === true) {
        outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Found multihash advertised in <span class='font-bold'>${peerResult.ProviderRecordFromPeerInDHT ? 'DHT' : 'IPNI'}</span></span></div>`
    } else {
        outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not find the multihash in DHT or IPNI</span></div>`
    }

    // Bitswap
    if (peerResult.DataAvailableOverBitswap?.Enabled === true) {
      if (peerResult.DataAvailableOverBitswap?.Error !== "") {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>There was an error downloading the data for the CID from the peer via Bitswap: <span class='font-mono'>${peerResult.DataAvailableOverBitswap.Error}</span></span></div>`
      } else if (peerResult.DataAvailableOverBitswap?.Responded !== true) {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>The peer did not quickly respond if it had the data for the CID over Bitswap</span></div>`
      } else if (peerResult.DataAvailableOverBitswap?.Found === true) {
          outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex items-center'>${iconCheck}<span>The peer responded that it has the data for the CID over Bitswap</span></div>`
      } else {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>The peer responded that it does not have the data for the CID over Bitswap</span></div>`
      }
    }

    // HTTP
    if (peerResult.DataAvailableOverHTTP?.Enabled === true) {
      if (peerResult.DataAvailableOverHTTP?.Error !== "") {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>There was an error downloading the data for the CID via HTTP: <span class='font-mono'>${peerResult.DataAvailableOverHTTP.Error}</span></span></div>`
      }

      if (peerResult.DataAvailableOverHTTP?.Connected !== true) {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>HTTP connection was unsuccessful to the HTTP endpoint</span></div>`
      } else if (peerResult.DataAvailableOverHTTP?.Found === true) {
          outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex items-center'>${iconCheck}<span>The HTTP endpoint responded that it has the data for the CID</span></div>`
      } else {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>The HTTP endpoint responded that it does not have the data for the CID</span></div>`
      }
    }
    outHtml += '</div>'
    return outHtml
}

function formatJustCidOutput (resp) {
    let outHtml = ''

    // Show resolution info if present (at top level)
    if (resp.MutableResolution) {
        outHtml += formatMutableResolution(resp.MutableResolution)
        // Handle resolution-only response (resolution failed, no providers)
        if (!resp.Providers) {
            return outHtml
        }
    }

    // Extract providers array (might be at top level or under Providers key)
    const providers = resp.Providers || resp

    if (!Array.isArray(providers) || providers.length === 0) {
        return outHtml + `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>No providers found for the given CID</span></div>`
    }

    const successfulProviders = providers.reduce((acc, provider) => {
        if(provider.ConnectionError === '' && (provider.DataAvailableOverBitswap?.Found === true || provider.DataAvailableOverHTTP?.Found === true)) {
            acc++
        }
        return acc
    }, 0)

    // Show providers with the data first, followed by reachable providers, then by those with addresses
    providers.sort((a, b) => {
        const aHasData = a.DataAvailableOverBitswap?.Found || a.DataAvailableOverHTTP?.Found
        const bHasData = b.DataAvailableOverBitswap?.Found || b.DataAvailableOverHTTP?.Found

        // First order by data availability
        if (aHasData && !bHasData) return -1
        if (!aHasData && bHasData) return 1

        const aHasBitswap = a.DataAvailableOverBitswap?.Enabled
        const bHasBitswap = b.DataAvailableOverBitswap?.Enabled

        // Then order HTTP first
        if (aHasBitswap && !bHasBitswap) return 1
        if (!aHasBitswap && bHasBitswap) return -1


        const aSource = a.Source
        const bSource = b.Source

        // Then order Amino DHT first
        if (aSource === 'IPNI' && bSource !== 'IPNI') return 1
        if (aSource !== 'IPNI' && bSource === 'IPNI') return -1

        // Then order by connection errors
        if (a.ConnectionError === '' && b.ConnectionError !== '') {
            return -1;
        } else if (a.ConnectionError !== '' && b.ConnectionError === '') {
            return 1;
        }

        // Finally, show providers with addresses first
        if(a.Addrs?.length > 0 && b.Addrs?.length === 0) {
            return -1
        } else if(a.Addrs?.length === 0 && b.Addrs?.length > 0) {
            return 1
        }

        return 0
    })

    outHtml += `<div class='mb-4'><span class='text-lg font-bold'>${successfulProviders > 0 ? iconCheck : iconCross} Found ${successfulProviders} working providers</span> <span class='text-gray-600'>(out of ${providers.length} provider records sampled from Amino DHT and IPNI) that could be connected to and had the CID available over Bitswap:</span></div>`

    // If every returned provider record lacked any usable address, surface a
    // hint. This typically means the closest DHT peers held stale provider
    // records and the FindPeer fallback could not find the peer either, so
    // the network has nothing fresh to dial.
    const allNoAddrs = providers.length > 0 && providers.every(p => !p.Addrs || p.Addrs.length === 0)
    if (allNoAddrs) {
        outHtml += `<div class='bg-yellow-100 border-l-4 border-yellow-500 text-yellow-800 p-4 rounded mb-4 flex gap-x-2 items-start'>${iconInfo}<span>None of the returned provider records contained a current multiaddr. The records are likely stale (providers re-advertised the CID long ago and have since gone offline or changed addresses), or the routing layer did not have fresh peer information. Try again later, ask the publisher to re-provide, or test against a different routing endpoint in <b>Backend Config</b>.</span></div>`
    }

    outHtml += `<div class='grid gap-4 grid-cols-1'>`
    for (const provider of providers) {
        const couldConnect = provider.ConnectionError === ''
        const hasBitswap = provider.DataAvailableOverBitswap?.Enabled === true
        const hasHTTP = provider.DataAvailableOverHTTP?.Enabled === true
        const foundBitswap = provider.DataAvailableOverBitswap?.Found
        const foundHTTP = provider.DataAvailableOverHTTP?.Found
        const isUnsuccessful = !couldConnect || (!foundBitswap && !foundHTTP)
        const cardBg = isUnsuccessful ? 'bg-red-50 border-red-200' : 'bg-white border-gray-200'
        outHtml += `<div class='rounded-lg shadow ${cardBg} p-4 border'>`
        outHtml += `<div class='flex justify-between items-center mb-2'>`
        outHtml += `<span class='font-mono text-xs bg-gray-100 px-2 py-1 rounded mr-2 break-all'>${provider.ID}</span>`
        if (hasBitswap) outHtml += `<span class='ml-2 px-2 py-1 rounded bg-green-100 text-green-700 text-xs font-bold'>Bitswap</span>`
        if (hasHTTP) outHtml += `<span class='ml-2 px-2 py-1 rounded bg-blue-100 text-blue-700 text-xs font-bold'>HTTP</span>`
        if (provider?.Source != null) {
            const bgColor = provider.Source === 'IPNI' ? 'bg-orange-100 text-orange-700' : 'bg-purple-100 text-purple-700'
            outHtml += `<span class='ml-2 px-2 py-1 rounded ${bgColor} text-xs font-bold'>${provider.Source}</span>`
        }
        outHtml += `</div>`
        if (hasBitswap) {
            outHtml += `<div class='flex items-center text-sm mb-1'>${couldConnect ? iconCheck : iconCross}<span>Libp2p connected: <span class='font-mono'>${couldConnect ? 'Yes' : provider.ConnectionError.replaceAll('\n', '<br>')}</span></span></div>`
            if (couldConnect) {
                if (provider.AgentVersion) {
                    outHtml += `<div class='flex items-center text-sm mb-1 ml-6'>${iconCheck}<span>Agent Version: <span class='font-mono'>${provider.AgentVersion}</span></span></div>`
                }
                const foundText = provider.DataAvailableOverBitswap.Found ? 'Found' : 'Not found'
                outHtml += `<div class='flex items-center text-sm mb-1 ml-6'>${provider.DataAvailableOverBitswap.Found ? iconCheck : iconCross}<span>Bitswap Check: <span class='font-mono'>${foundText}</span> ${provider.DataAvailableOverBitswap.Error || ''}</span></div>`
            }
        }
        if (hasHTTP) {
            const httpRes = provider.DataAvailableOverHTTP
            outHtml += `<div class='flex items-center text-sm mb-1'>${httpRes?.Connected ? iconCheck : iconCross}<span>HTTP Connected: <span class='font-mono'>${httpRes?.Connected ? 'Yes' : 'No'}</span></span></div>`
            outHtml += `<div class='flex items-center text-sm mb-1 ml-6'>${httpRes?.Requested ? iconCheck : iconCross}<span>HTTP request: <span class='font-mono'>${httpRes?.Requested ? 'Yes' : 'No'}</span></span></div>`
            outHtml += `<div class='flex items-center text-sm mb-1 ml-6'>${httpRes?.Found ? iconCheck : iconCross}<span>HTTP Found: <span class='font-mono'>${httpRes?.Found ? 'Yes' : 'No'}</span> ${httpRes?.Error ? '(' + httpRes.Error + ')' : ''}</span></div>`
        }
        outHtml += (couldConnect && provider.ConnectionMaddrs) ? `<div class='text-xs text-gray-600 mt-2'><span class='font-bold'>${(hasHTTP && !provider.DataAvailableOverHTTP?.Found) ? 'Attempted' : 'Successful'} Connection Multiaddr${provider.ConnectionMaddrs.length > 1 ? 's' : ''}:</span><br><span class='font-mono block ml-4 break-all whitespace-break-spaces'>${provider.ConnectionMaddrs?.join('<br>') || ''}</span></div>` : ''
        outHtml += (provider.Addrs?.length > 0) ? `<div class='text-xs text-gray-600 mt-2'><span class='font-bold'>Peer Multiaddrs:</span><br><span class='font-mono block ml-4 break-all whitespace-break-spaces'>${provider.Addrs.join('<br>') || ''}</span></div>` : ''
        outHtml += `</div>`
    }
    outHtml += `</div>`
    return outHtml
}

/**
 * ----------------------------------------------------------------------------------------------------------
 * If included in an iframe, we need to allow consumers/parent frames to know the size of the iframe content.
 * e.g. ipfs-webui's /#/diagnostics/check page, where ipfs-check is embedded
 * ----------------------------------------------------------------------------------------------------------
 */
if (window.self !== window.top) {
  let rafId = null;
  let lastH = -1;

  function measuredHeight() {
    const sentinel = document.getElementById('__iframe_sentinel');
    if (!sentinel) {
      // Fallback to document height if sentinel is missing
      return document.documentElement.getBoundingClientRect().height;
    }
    const rect = sentinel.getBoundingClientRect();
    const bottom = rect.bottom + window.scrollY; // page Y of sentinel bottom
    const documentElHeight = document.documentElement.getBoundingClientRect().height
    return Math.min(bottom, documentElHeight);
  }

  function postSize() {
    if (rafId != null) return;
    rafId = requestAnimationFrame(() => {
      rafId = null;
      const h = measuredHeight();
      if (h !== lastH) {
        lastH = h;
        try {
          parent.postMessage({ type: 'iframe-size:report', width: document.documentElement.scrollWidth, height: h, scrollHeight: document.documentElement.scrollHeight, scrollWidth: document.documentElement.scrollWidth }, '*');
        } catch (error) {
          // Silently fail - iframe might be sandboxed or parent might not exist
          console.debug('Failed to post size to parent:', error);
        }
      }
    });
  }

  // triggers
  window.addEventListener('load', postSize);
  window.addEventListener('resize', postSize);
  window.visualViewport?.addEventListener('resize', postSize);
  document.addEventListener('transitionend', postSize);
  document.fonts?.addEventListener?.('loadingdone', postSize);

  // Store observers for cleanup
  const resizeObserver = new ResizeObserver(postSize);
  const mutationObserver = new MutationObserver(postSize);
  
  resizeObserver.observe(document.body);
  mutationObserver.observe(document.body, { childList: true, subtree: true });
  
  // Cleanup on page unload
  window.addEventListener('beforeunload', () => {
    resizeObserver.disconnect();
    mutationObserver.disconnect();
    if (rafId != null) {
      cancelAnimationFrame(rafId);
    }
  });
}
