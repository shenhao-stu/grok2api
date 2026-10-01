import type {DashboardDTO} from './dashboard-api';

export function WelfareLedger({dashboard,locale}:{dashboard?:DashboardDTO;locale:string}) {
 const zh=locale.startsWith('zh');const ledger=dashboard?.welfareLedger;
 const text=(cn:string,en:string)=>zh?cn:en;
 const nf=(value:string|number)=>new Intl.NumberFormat(locale,{maximumFractionDigits:3}).format(Number(value));
 return <section className="rounded-xl border bg-card p-5 space-y-4">
  <div><h2 className="text-sm font-medium">{text('公益站今日账本 · 北京时间','Welfare ledger today · Beijing time')}</h2><p className="mt-2 text-xs leading-5 text-muted-foreground">{text('读取公益站同一份已结算账本。上方美元金额是模型官方定价的参考估算，不是支付账单或站内扣点；中继重试和运维探测也会出现在上游请求数中。','Read from the same settled Welfare ledger. USD values above are reference estimates, not an invoice or site-credit charges. Relay retries and operational probes also count as upstream requests.')}</p></div>
  {!ledger?<p role="status" className="text-xs text-muted-foreground">{text('账本暂未同步，稍后自动重试。','Ledger unavailable. Retrying automatically.')}</p>:<><div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">{[
   [text('实际消费','Settled consumption'),ledger.consumed],
   [text('赠送到账','Granted credit'),ledger.granted],
   [text('到期回收','Expired credit'),ledger.expired],
   [text('创建兑换码面值','Created code face value'),ledger.inventoryCreated],
  ].map(([label,value])=><div key={label} className="rounded-lg border bg-background/40 p-4"><p className="text-xs text-muted-foreground">{label}</p><strong className="mt-2 block text-xl font-medium tabular-nums">{nf(value)} <small className="text-xs font-normal">{text('点','pts')}</small></strong></div>)}</div><p className="text-xs text-muted-foreground">{text('兑换到账','Redeemed')}: {nf(ledger.redeemed)} · {text('签到到账','Check-in')}: {nf(ledger.checkin)} · {text('同步于','Updated')} {new Date(ledger.updatedAt).toLocaleString(locale,{timeZone:'Asia/Shanghai',hour12:false})}</p></>}
 </section>;
}
