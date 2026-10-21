'use strict';
// Offline numbering-plan checks. These never contact Telegram or request a code.
const NumberQuality=(()=>{
 const cache=new Map(),storageKey='free-number-telegram-reports-v1';
 let reports=Object.create(null);
 const states=new Set(['invalid','banned','code-received']);
 try{const saved=JSON.parse(localStorage.getItem(storageKey)||'{}');if(saved&&typeof saved==='object'&&!Array.isArray(saved))for(const [number,r] of Object.entries(saved)){if(/^\+[1-9][0-9]{6,14}$/.test(number)&&r&&states.has(r.state)&&Number.isFinite(r.at)&&r.at>0&&r.at<=Date.now())reports[number]={state:r.state,at:r.at}}}catch{}
 function inspect(raw){
  const input=String(raw??'').trim();if(cache.has(input))return cache.get(input);
  const normalized=input.replace(/[۰-۹]/g,c=>String(c.charCodeAt(0)-1776)).replace(/[٠-٩]/g,c=>String(c.charCodeAt(0)-1632)).replace(/[ ()-]/g,'').replace(/^00/,'+');
  let result={number:normalized,state:'unknown',reason:'قالب شماره هنوز تأیید نشده'};
  if(!/^\+[1-9][0-9]{6,14}$/.test(normalized))result={number:normalized,state:'invalid',reason:'قالب بین‌المللی یا تعداد ارقام نامعتبر است'};
  else if(typeof libphonenumber!=='undefined'){
   try{const p=libphonenumber.parsePhoneNumberFromString(normalized);
    if(!p||!p.isPossible())result={number:normalized,state:'invalid',reason:'کد کشور یا تعداد ارقام با طرح شماره‌گذاری سازگار نیست'};
    else result={number:p.number,state:p.isValid()?'valid':'unknown',reason:p.isValid()?'ساختار شماره مطابق طرح شماره‌گذاری است؛ پذیرش تلگرام بررسی نشده':'طول شماره ممکن است؛ الگوی شماره در داده‌های فعلی تأیید نشده'};
   }catch{result={number:normalized,state:'unknown',reason:'بررسی ساختار شماره کامل نشد'}}
  }
  if(cache.size>=10000)cache.clear();cache.set(input,result);return result;
 }
 function report(raw){return reports[inspect(raw).number]||null}
 function setReport(raw,state){
  const number=inspect(raw).number;if(!/^\+[1-9][0-9]{6,14}$/.test(number))return false;
  if(state==='clear')delete reports[number];else if(states.has(state))reports[number]={state,at:Date.now()};else return false;
  try{localStorage.setItem(storageKey,JSON.stringify(reports));return true}catch{return false}
 }
 function telegramState(raw,history){const r=report(raw);if(r)return r.state;
  return history&&!history.error&&history.counts?.[1]>0?'message-observed':'unchecked';
 }
 function matches(raw,history,mode){const s=telegramState(raw,history),rejected=s==='invalid'||s==='banned';
  if(mode==='all')return true;if(mode==='rejected')return rejected;if(mode==='observed')return s==='message-observed'||s==='code-received';if(mode==='unchecked')return s==='unchecked';return !rejected;
 }
 function importReports(saved){reports=Object.create(null);if(saved&&typeof saved==='object')for(const [number,r] of Object.entries(saved))if(/^\+[1-9][0-9]{6,14}$/.test(number)&&r&&states.has(r.state)&&Number.isFinite(r.at)&&r.at>0&&r.at<=Date.now()+60000)reports[number]={state:r.state,at:r.at}}
 return{inspect,report,setReport,telegramState,matches,importReports};
})();
