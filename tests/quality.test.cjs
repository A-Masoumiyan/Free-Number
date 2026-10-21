const {test}=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),vm=require('node:vm');
const libphonenumber=require('libphonenumber-js/max');
const source=fs.readFileSync(require('node:path').join(__dirname,'../ui/number-quality.js'),'utf8');
function setup(saved='{}',failStorage=false){let value=saved;const ctx={libphonenumber,localStorage:{getItem:()=>value,setItem:(_,v)=>{if(failStorage)throw Error('quota');value=v}}};vm.createContext(ctx);vm.runInContext(source+';this.quality=NumberQuality',ctx);return{q:ctx.quality,saved:()=>value}}
test('offline validation keeps plausible unknown ranges but removes impossible numbers',()=>{
 const {q}=setup();assert.equal(q.inspect('+447911123456').state,'valid');assert.equal(q.inspect('+999123456789').state,'invalid');assert.equal(q.inspect('+1202').state,'invalid');assert.equal(q.inspect('+1202555012345678').state,'invalid');assert.equal(q.inspect('+44abc7700900123').state,'invalid');assert.equal(q.inspect('+447700900123').state,'unknown');assert.equal(q.inspect('۰۰۴۴ ۷۷۰۰ ۹۰۰۱۲۳').number,'+447700900123');
});
test('reports persist by normalized number and override message history without claiming verified acceptance',()=>{
 const {q,saved}=setup();const h={counts:[0,1],checkedAt:new Date().toISOString()};
 assert.equal(q.telegramState('+447700900123',h),'message-observed');assert.equal(q.setReport('+44 7700 900123','banned'),true);
 assert.equal(q.matches('+447700900123',h,'candidates'),false);assert.equal(q.matches('+447700900123',h,'rejected'),true);
 const next=setup(saved()).q;assert.equal(next.telegramState('+447700900123',h),'banned');assert(next.report('+447700900123').at>0);
 next.setReport('+447700900123','clear');assert.equal(next.telegramState('+447700900123',h),'message-observed');
 next.setReport('+447700900123','code-received');assert.equal(next.matches('+447700900123',null,'observed'),true);
 assert.equal(next.telegramState('+447700900124',{error:'offline',counts:[0,2]}),'unchecked');assert.equal(next.telegramState('+447700900124',{counts:[0,0]}),'unchecked');
});
test('corrupt storage and quota failures preserve usable UI state',()=>{
 for(const v of ['null','[]','oops','{"+447700900123":{"state":"verified","at":1}}'])assert.equal(setup(v).q.report('+447700900123'),null);
 const {q}=setup('{}',true);assert.equal(q.setReport('+447700900123','invalid'),false);assert.equal(q.telegramState('+447700900123',null),'invalid');
});
