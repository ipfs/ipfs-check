// SVG icons for status
const iconCheck = `<svg class="inline w-5 h-5 text-green-500 mr-1" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"/></svg>`
const iconCross = `<svg class="inline w-5 h-5 text-red-500 mr-1" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12"/></svg>`
const iconInfo = `<svg class="inline w-5 h-5 text-blue-500 mr-1" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" d="M13 16h-1v-4h-1m1-4h.01"/></svg>`

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
        startCountdown(timeoutSeconds)

        plausible('IPFS Check Run', {
            props: {
                withMultiaddr: inputMaddr !== ''
            },
        })

        showInQuery(formData) // add `cid` and `multiaddr` to local url query to make it shareable
        toggleSubmitButton()
        try {
          const res = await fetch(backendURL, { method: 'POST' })

          if (res.ok) {
              const respObj = await res.json()
              showRawOutput(JSON.stringify(respObj, null, 2))

              if(inputMaddr === '') {
                const output = formatJustCidOutput(respObj)
                showOutput(output)
              } else {
                const output = formatMaddrOutput(inputMaddr, respObj)
                showOutput(output)
              }
          } else {
              const resText = await res.text()
              showOutput(`⚠️ backend returned an error: ${res.status} ${resText}`)
          }
        } catch (e) {
          console.log(e)
          showOutput(`⚠️ backend error: ${e}`)
        } finally {
          stopCountdown()
          toggleSubmitButton()
        }
    })
    
    function startCountdown(seconds) {
        // Clear any existing countdown
        stopCountdown()
        
        const buttonText = document.getElementById('button-text')
        let remaining = seconds
        
        // Update button text immediately
        if (buttonText) {
            buttonText.textContent = `Testing: ${remaining}s`
        }
        
        // Update every second
        countdownInterval = setInterval(() => {
            remaining--
            if (remaining <= 0) {
                stopCountdown()
            } else if (buttonText) {
                buttonText.textContent = `Testing: ${remaining}s`
            }
        }, 1000)
    }
    
    function stopCountdown() {
        if (countdownInterval) {
            clearInterval(countdownInterval)
            countdownInterval = null
        }
        
        // Restore original button text
        const buttonText = document.getElementById('button-text')
        if (buttonText) {
            buttonText.textContent = 'Run Test'
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

function formatMaddrOutput (multiaddr, respObj) {
    const peerIDStartIndex = multiaddr.lastIndexOf("/p2p/")
    const peerID = multiaddr.slice(peerIDStartIndex + 5);
    const addrPart = multiaddr.slice(0, peerIDStartIndex);
    let outHtml = `<div class='space-y-4'>`

    // Connection status
    if (respObj.ConnectionError !== "") {
        outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not connect to multiaddr: <span class='font-mono'>${respObj.ConnectionError}</span></span></div>`
    } else {
        const madrs = respObj?.ConnectionMaddrs
        outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Successfully connected to multiaddr${madrs?.length > 1 ? 's' : '' }:<br><span class='font-mono text-xs block ml-6'>${madrs.join('<br>')}</span></span></div>`
    }

    // DHT status
    if (multiaddr.indexOf("/p2p/") === 0 && multiaddr.lastIndexOf("/") === 4) {
        // only peer id passed with /p2p/PeerID
        if (Object.keys(respObj.PeerFoundInDHT).length === 0) {
            outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not find any multiaddrs in the DHT</span></div>`
        } else {
            outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Found multiaddrs advertised in the DHT:<br><span class='font-mono text-xs block ml-6'>${Object.keys(respObj.PeerFoundInDHT).join('<br>')}</span></span></div>`
        }
    } else {
        // a proper maddr with an IP was passed
        let foundAddr = false
        for (const key in respObj.PeerFoundInDHT) {
            if (key === addrPart) {
                foundAddr = true
                outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Found multiaddr with <span class='font-bold'>${respObj.PeerFoundInDHT[key]}</span> DHT peers</span></div>`
                break
            }
        }
        if (!foundAddr) {
            let alt = ''
            if (Object.keys(respObj.PeerFoundInDHT).length > 0) {
              alt = `<br>Instead found:<br><span class='font-mono text-xs block ml-6 break-all'>${Object.keys(respObj.PeerFoundInDHT).join('<br>')}</span>`
            } else {
              alt = '<br>No other addresses were found.'
            }
            outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not find the given multiaddr in the DHT.${alt}</span></div>`
        }
    }

    // Provider record
    if (respObj.ProviderRecordFromPeerInDHT === true || respObj.ProviderRecordFromPeerInIPNI === true) {
        outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex gap-x-2 items-center'>${iconCheck}<span>Found multihash advertised in <span class='font-bold'>${respObj.ProviderRecordFromPeerInDHT ? 'DHT' : 'IPNI'}</span></span></div>`
    } else {
        outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex gap-x-2 items-center'>${iconCross}<span>Could not find the multihash in DHT or IPNI</span></div>`
    }

    // Bitswap
    if (respObj.DataAvailableOverBitswap?.Enabled === true) {
      if (respObj.DataAvailableOverBitswap?.Error !== "") {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>There was an error downloading the data for the CID from the peer via Bitswap: <span class='font-mono'>${respObj.DataAvailableOverBitswap.Error}</span></span></div>`
      } else if (respObj.DataAvailableOverBitswap?.Responded !== true) {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>The peer did not quickly respond if it had the data for the CID over Bitswap</span></div>`
      } else if (respObj.DataAvailableOverBitswap?.Found === true) {
          outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex items-center'>${iconCheck}<span>The peer responded that it has the data for the CID over Bitswap</span></div>`
      } else {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>The peer responded that it does not have the data for the CID over Bitswap</span></div>`
      }
    }

    // HTTP
    if (respObj.DataAvailableOverHTTP?.Enabled === true) {
      if (respObj.DataAvailableOverHTTP?.Error !== "") {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>There was an error downloading the data for the CID via HTTP: <span class='font-mono'>${respObj.DataAvailableOverHTTP.Error}</span></span></div>`
      }

      if (respObj.DataAvailableOverHTTP?.Connected !== true) {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>HTTP connection was unsuccessful to the HTTP endpoint</span></div>`
      } else if (respObj.DataAvailableOverHTTP?.Found === true) {
          outHtml += `<div class='bg-green-100 border-l-4 border-green-500 text-green-700 p-4 rounded flex items-center'>${iconCheck}<span>The HTTP endpoint responded that it has the data for the CID</span></div>`
      } else {
          outHtml += `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>The HTTP endpoint responded that it does not have the data for the CID</span></div>`
      }
    }
    outHtml += '</div>'
    return outHtml
}

function formatJustCidOutput (resp) {
    if (resp.length === 0) {
        return `<div class='bg-red-100 border-l-4 border-red-500 text-red-700 p-4 rounded flex items-center'>${iconCross}<span>No providers found for the given CID</span></div>`
    }

    const successfulProviders = resp.reduce((acc, provider) => {
        if(provider.ConnectionError === '' && (provider.DataAvailableOverBitswap?.Found === true || provider.DataAvailableOverHTTP?.Found === true)) {
            acc++
        }
        return acc
    }, 0)

    // Show providers with the data first, followed by reachable providers, then by those with addresses
    resp.sort((a, b) => {
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

    let outHtml = `<div class='mb-4'><span class='text-lg font-bold'>${successfulProviders > 0 ? iconCheck : iconCross} Found ${successfulProviders} working providers</span> <span class='text-gray-600'>(out of ${resp.length} provider records sampled from Amino DHT and IPNI) that could be connected to and had the CID available over Bitswap:</span></div>`
    outHtml += `<div class='grid gap-4 grid-cols-1'>`
    for (const provider of resp) {
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
            outHtml += `<div class='flex items-center text-sm mb-1 ml-6'>${httpRes?.Requested ? iconCheck : iconCross}<span>HTTP HEAD request: <span class='font-mono'>${httpRes?.Requested ? 'Yes' : 'No'}</span></span></div>`
            outHtml += `<div class='flex items-center text-sm mb-1 ml-6'>${httpRes?.Found ? iconCheck : iconCross}<span>HTTP Found: <span class='font-mono'>${httpRes?.Found ? 'Yes' : 'No'}</span> ${httpRes?.Error ?? ''}</span></div>`
        }
        outHtml += (couldConnect && provider.ConnectionMaddrs) ? `<div class='text-xs text-gray-600 mt-2'><span class='font-bold'>Successful Connection Multiaddr${provider.ConnectionMaddrs.length > 1 ? 's' : ''}:</span><br><span class='font-mono block ml-4 break-all whitespace-break-spaces'>${provider.ConnectionMaddrs?.join('<br>') || ''}</span></div>` : ''
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
