<script setup lang="ts">
// The Syrius extension as the lab sees it, and the accounts every bench signs
// from. An unfunded wallet and a broken integration look the same from a page
// (the node refuses the block and the wallet says so only in its own window),
// so balance, plasma and unreceived blocks are shown here next to the wallet.
import { computed, reactive, ref } from 'vue'
import HexValue from '../components/HexValue.vue'
import {
  chainId,
  connectWallet,
  disconnectWallet,
  formatUnits,
  fuse,
  labKeys,
  receiveAll,
  refreshAccounts,
  refreshWallet,
  settled,
  state,
  transfer,
  type SignerId,
} from '../store.ts'
import { unlockMessage } from '../ptlc/contract.ts'
import { signMessage } from '../wallet/syrius.ts'
import { QSR_ZTS, ZNN_ZTS, toHex } from '../znn/primitives.ts'
import { errorText } from '../lib.ts'
import { DEFAULT_NODE, node } from '../node/node.ts'

defineProps<{ param: string }>()

const w = computed(() => state.wallet)

interface Check {
  state: 'ok' | 'no' | 'unknown'
  title: string
  detail: string
}

const checks = computed<Check[]>(() => {
  const list: Check[] = []
  list.push(
    w.value.present
      ? {
          state: 'ok',
          title: 'The extension is in this browser',
          detail: w.value.extensionVersion
            ? `Syrius ${w.value.extensionVersion}, provider protocol ${w.value.protocol}.`
            : `Provider protocol ${w.value.protocol}. The release is not something window.zenon reports; only v0.4.0 and later say, through window.ethereum.syrius.`,
        }
      : {
          state: 'no',
          title: 'No Zenon wallet in this browser',
          detail: 'Install Syrius v0.3.4 or later (load its build directory unpacked) and reload this page. The keys the page holds work without it.',
        },
  )
  if (!w.value.present) return list

  list.push(
    w.value.address
      ? { state: 'ok', title: 'Connected to this page', detail: `As ${w.value.address}. The connection is to that one account; switch accounts in Syrius and this page is shown nothing until it is approved for the new one.` }
      : { state: 'no', title: 'Not connected', detail: 'Syrius tells a site nothing, not even its chain, until you approve it. Connect, and unlock the wallet if it asks.' },
  )
  if (!w.value.address) return list

  if (node.source !== 'wallet') {
    list.push({ state: 'no', title: 'It has not said which node it is on', detail: 'Syrius reports its node to a connected page, and this page reads that node. Without it the page cannot follow what the wallet publishes, and does not sign through it.' })
    return list
  }
  list.push(
    node.problem
      ? { state: 'no', title: 'This page cannot dial its node', detail: node.problem }
      : state.nodeError
        ? { state: 'no', title: 'Its node is not answering this page', detail: `${node.url}: ${state.nodeError}. Syrius reaches its node from the extension, which no page's rules bind; this page has to reach it too, to show what the wallet published.` }
        : { state: 'ok', title: 'The page reads its node', detail: `${node.url}. While Syrius is connected this is the only node the page reads, so what you see here and what Syrius signs for are one chain. Change the node in Syrius and the page follows.` },
  )
  if (state.momentum && !state.nodeError) {
    list.push(
      w.value.chainId === chainId()
        ? { state: 'ok', title: `Signing for chain ${chainId()}`, detail: 'The chain identifier goes into every block hash; a block signed for another chain is refused.' }
        : { state: 'no', title: `Syrius signs for chain ${w.value.chainId ?? 'unknown'}, its node is chain ${chainId()}`, detail: `On the screen where Syrius lists its nodes it reads the node's chain and offers to use it. Take that: a block signed for chain ${w.value.chainId ?? 'unknown'} is refused by a node on chain ${chainId()}.` },
    )
    list.push(
      state.spork
        ? { state: 'ok', title: 'The ptlc contract is active there', detail: 'The spork that turns the contract on is active on this chain.' }
        : state.spork === false
          ? { state: 'no', title: 'The ptlc contract is not active on that chain', detail: `A Create sent there is a transfer to an address nothing answers at. A devnet on the ptlc branch has it; the default is ${DEFAULT_NODE}.` }
          : { state: 'unknown', title: 'Reading whether the ptlc contract is active there', detail: 'The page is asking the node for its sporks.' },
    )
  }
  const account = state.accounts[w.value.address]
  if (account) {
    const znn = BigInt(account.balances[ZNN_ZTS] ?? '0')
    const qsr = BigInt(account.balances[QSR_ZTS] ?? '0')
    list.push(
      znn > 0n || qsr > 0n
        ? { state: 'ok', title: 'It holds something to lock', detail: `${formatUnits(znn, 8)} ZNN and ${formatUnits(qsr, 8)} QSR on its node.` }
        : { state: 'no', title: 'It holds nothing', detail: 'An unlock or a reclaim sends no funds and will work; a Create needs a balance. Fund it from Alice below.' },
    )
    list.push(
      account.plasma > 0
        ? { state: 'ok', title: 'It has plasma', detail: `${account.plasma.toLocaleString('en')} of ${account.maxPlasma.toLocaleString('en')}. Its blocks need no proof of work.` }
        : { state: 'unknown', title: 'It has no plasma', detail: 'Syrius will mine proof of work for each block, which takes a minute or more in a browser. Fuse QSR for it from Alice below to skip that.' },
    )
    if (account.unreceived > 0) {
      list.push({ state: 'unknown', title: `${account.unreceived} block${account.unreceived === 1 ? '' : 's'} waiting to be received`, detail: 'Syrius publishes receive blocks once each time its dashboard is opened. Open the extension, or leave its dashboard and come back to it.' })
    }
  }
  return list
})

// ---- signMessage -----------------------------------------------------------------

const sign = reactive({ busy: false, result: '', ok: true })
/** 32 characters of ASCII, so 32 bytes: the size of the digest a key-type witness signs. */
const digest = computed(() => toHex(unlockMessage(chainId(), 0, '11'.repeat(32), w.value.address ?? labKeys[0].account.address)))
async function trySign(message: string) {
  sign.busy = true
  sign.result = ''
  try {
    const out = await signMessage(message, {
      onHold: (seconds) => {
        sign.result = seconds > 0 ? `Syrius is holding this site off for another ${seconds} s after the last window; asking again then.` : ''
      },
    })
    sign.ok = true
    sign.result = `signed by ${out.address}\npublic key ${out.publicKey}\nsignature  ${out.signature}`
  } catch (err) {
    sign.ok = false
    sign.result = errorText(err)
  } finally {
    sign.busy = false
  }
}

// ---- accounts --------------------------------------------------------------------

const rows = computed(() => {
  const list: { id: SignerId; name: string; what: string; address: string }[] = labKeys.map((k) => ({
    id: k.id,
    name: k.name,
    what: k.what,
    address: k.account.address,
  }))
  if (w.value.address && !list.some((r) => r.address === w.value.address)) {
    list.push({ id: 'syrius', name: 'Syrius', what: 'the account the extension is on', address: w.value.address })
  }
  return list
})

const busy = ref('')
const error = ref('')

async function run(name: string, job: () => Promise<unknown>) {
  busy.value = name
  error.value = ''
  try {
    await job()
    await refreshAccounts()
  } catch (err) {
    error.value = errorText(err)
  } finally {
    busy.value = ''
  }
}

const FUND_ZNN = 100n * 10n ** 8n
const FUND_QSR = 1000n * 10n ** 8n
const FUSE_QSR = 200n * 10n ** 8n

const fund = (address: string) =>
  run(`fund-${address}`, async () => {
    await settled(await transfer('lab:alice', address, FUND_ZNN, ZNN_ZTS))
    await settled(await transfer('lab:alice', address, FUND_QSR, QSR_ZTS))
  })
const fuseFor = (address: string) => run(`fuse-${address}`, async () => void (await settled(await fuse('lab:alice', address, FUSE_QSR))))
const receive = (id: SignerId) => run(`receive-${id}`, () => receiveAll(id))
const balance = (address: string, zts: string) => formatUnits(state.accounts[address]?.balances[zts] ?? '0', 8)
</script>

<template>
  <h1 class="bench-title">Syrius and accounts</h1>
  <p class="bench-lede">
    Syrius signs any account block a page hands it, and a ptlc call is an account block. So the extension can make all four calls without knowing the contract exists. This bench is where it gets connected, checked and funded.
  </p>

  <section class="panel">
    <div class="panel-head">
      <h2 class="section-title">The extension</h2>
      <div class="btn-row">
        <button v-if="w.present && !w.address" type="button" class="btn btn-primary" :disabled="w.busy" @click="connectWallet">
          {{ w.busy ? 'Waiting for Syrius' : 'Connect Syrius' }}
        </button>
        <template v-if="w.address">
          <button type="button" class="btn btn-quiet" @click="refreshWallet()">Check again</button>
          <button type="button" class="btn btn-quiet" @click="disconnectWallet">Disconnect</button>
        </template>
      </div>
    </div>
    <ul class="checks">
      <li v-for="c in checks" :key="c.title" :class="c.state">
        <strong>{{ c.title }}</strong>
        <span>{{ c.detail }}</span>
      </li>
    </ul>
    <p v-if="w.error" class="note bad" style="margin-top: 10px" role="alert">{{ w.error }}</p>
  </section>

  <section class="panel">
    <h2 class="section-title">The wallet decides the node</h2>
    <div class="prose small">
      <p>
        Connecting Syrius moves this page onto the wallet's node, whatever the node menu says, and disconnecting moves it back. A wallet publishes what it signs to its own node; a page that read a different one would show a block as never arriving, or worse, show another chain's entries beside a signature made for this one.
      </p>
      <p>
        So the node is chosen in Syrius. For a devnet on this machine: Settings, the node list, add <HexValue :value="DEFAULT_NODE" full />, and take the chain id Syrius reads from it. In v0.4.0 stay on the network called Zenon Mainnet, which is the one that uses that list; the network called Zenon Devnet is a different chain 69, and the page will follow Syrius there too and tell you whether the ptlc contract exists on it.
      </p>
      <p>
        The chain in this page is not a node a wallet can dial, so Syrius cannot sign for it. The keys the page holds can.
      </p>
      <p>
        To have funds on a devnet without asking Alice, import the devnet mnemonic into Syrius and pick index 1 or 3; then Syrius is the same account as that key here, and the two should not sign at the same moment.
      </p>
    </div>
  </section>

  <section class="panel">
    <h2 class="section-title">What approving a block looks like</h2>
    <div class="prose small">
      <p>
        Every block opens Syrius's approval window. Since v0.3.4 that window lists the decoded arguments of a call to a contract it knows. ptlc is not one of them, so the window names the contract as <strong>unrecognized</strong> and warns that “this wallet cannot fully interpret this call or its data”, above the raw block. That is the expected screen, not a fault. Compare the destination, the amount and the data with the call preview on the bench that sent it: they are the same bytes.
      </p>
      <p>
        An approval is for one block, lasts 30 minutes, and belongs to this page as it is now. Reloading the page, or leaving it, cancels it. Moving between benches does not.
      </p>
      <p>
        After a window that was declined or closed, Syrius will not raise another for the same site for 30 seconds: a request inside that time is refused at once, with no window. The lab holds such a request and makes it again when the time is up, and says so in the list on the right. An approved block buys the next request an immediate window, so a run of approvals is not slowed.
      </p>
      <p>
        Syrius reads the account's frontier when the window opens and signs exactly what it showed. If another block lands on that account while you are reading, the node refuses the one you approve, and the call is simply made again.
      </p>
    </div>
  </section>

  <section class="panel">
    <h2 class="section-title">What Syrius will not sign</h2>
    <p class="small prose">
      A key lock opens with a signature over a 32-byte digest. Syrius's <code>signMessage</code> signs the bytes of a text, and refuses any text of exactly 32 bytes, because a block hash is 32 bytes and signing one would be signing a transaction. So the key of a Syrius account cannot be the lock of an Ed25519 entry opened through the extension. The lab's own keys hold those secrets, and Syrius sends the block that carries the signature.
    </p>
    <div class="btn-row" style="margin-top: 10px">
      <button type="button" class="btn" :disabled="!w.address || sign.busy" @click="trySign('ptlc lab: a 32-byte message here')">Ask it to sign 32 bytes</button>
      <button type="button" class="btn" :disabled="!w.address || sign.busy" @click="trySign(`ptlc lab unlock digest ${digest}`)">Ask it to sign a longer text</button>
      <span v-if="!w.address" class="small">Connect first.</span>
    </div>
    <pre v-if="sign.result" class="well" :class="{ bad: !sign.ok }" style="margin-top: 10px">{{ sign.result }}</pre>
  </section>

  <section class="panel">
    <div class="panel-head">
      <h2 class="section-title">Accounts</h2>
      <button type="button" class="btn btn-quiet" @click="refreshAccounts">Read again</button>
    </div>
    <div class="table-scroll">
      <table class="grid">
        <thead>
          <tr>
            <th>Account</th>
            <th>ZNN</th>
            <th>QSR</th>
            <th>Plasma</th>
            <th>Waiting</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in rows" :key="r.address">
            <td>
              <strong>{{ r.name }}</strong>
              <span v-if="r.id !== 'syrius' && r.address === w.address" class="tag">also Syrius</span>
              <br /><HexValue :value="r.address" :head="10" :tail="6" /><br />
              <span class="small faint">{{ r.what }}</span>
            </td>
            <td class="data">{{ balance(r.address, ZNN_ZTS) }}</td>
            <td class="data">{{ balance(r.address, QSR_ZTS) }}</td>
            <td class="data">{{ (state.accounts[r.address]?.plasma ?? 0).toLocaleString('en') }}</td>
            <td class="data">{{ state.accounts[r.address]?.unreceived ?? 0 }}</td>
            <td class="actions">
              <button
                v-if="r.id !== 'syrius'"
                type="button"
                class="btn btn-quiet"
                :disabled="!!busy || !(state.accounts[r.address]?.unreceived)"
                @click="receive(r.id)"
              >
                {{ busy === `receive-${r.id}` ? 'Receiving' : 'Receive' }}
              </button>
              <template v-if="r.id !== 'lab:alice' && r.id !== 'lab:bob'">
                <button type="button" class="btn btn-quiet" :disabled="!!busy" @click="fund(r.address)">
                  {{ busy === `fund-${r.address}` ? 'Sending' : 'Fund from Alice' }}
                </button>
                <button type="button" class="btn btn-quiet" :disabled="!!busy" @click="fuseFor(r.address)">
                  {{ busy === `fuse-${r.address}` ? 'Fusing' : 'Fuse plasma' }}
                </button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="small" style="margin-top: 10px; max-width: 70ch">
      Funding sends 100 ZNN and 1,000 QSR from Alice. Fusing locks 200 of Alice's QSR as plasma for the account, enough for its blocks to need no proof of work. A send is not a balance: the account has to publish a receive block, which a lab key does with Receive and Syrius does each time its dashboard is opened.
    </p>
    <p v-if="error" class="note bad" style="margin-top: 10px" role="alert">{{ error }}</p>
  </section>
</template>

<style scoped>
.checks {
  list-style: none;
  margin: 12px 0 0;
  padding: 0;
}
.checks li {
  position: relative;
  padding: 6px 0 6px 24px;
  border-top: 1px solid var(--line);
}
.checks li:first-child {
  border-top: 0;
}
.checks li::before {
  content: '';
  position: absolute;
  left: 3px;
  top: 0.95em;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--paid);
}
.checks li.no::before {
  background: var(--refused);
  border-radius: 1px;
}
.checks li.unknown::before {
  background: transparent;
  border: 2px solid var(--locked);
}
.checks strong {
  display: block;
}
.checks span {
  display: block;
  font-size: 0.86rem;
  color: var(--soft);
  max-width: 72ch;
  overflow-wrap: anywhere;
}
.setup {
  margin: 10px 0 0;
  padding-left: 20px;
  font-size: 0.92rem;
}
.setup li + li {
  margin-top: 6px;
}
.actions {
  white-space: nowrap;
  text-align: right;
}
.actions .btn + .btn {
  margin-left: 6px;
}
pre.bad {
  color: var(--refused);
}
</style>
