package org.freenumber.app;

import android.app.Activity;
import android.os.Bundle;
import android.graphics.Color;
import android.webkit.*;
import android.content.*;
import org.json.JSONObject;
import java.util.concurrent.*;
import java.io.*;
import java.net.*;
import javax.net.ssl.HttpsURLConnection;

public final class MainActivity extends Activity {
 private WebView web;
 private final ExecutorService workers=Executors.newFixedThreadPool(4);
 private volatile boolean destroyed=false;
 private static final String ORIGIN="https://appassets.androidplatform.net";
 @Override public void onCreate(Bundle state){super.onCreate(state);getWindow().setStatusBarColor(Color.rgb(11,17,32));getWindow().setNavigationBarColor(Color.rgb(11,17,32));
 web=new WebView(this);web.setBackgroundColor(Color.rgb(11,17,32));setContentView(web);
 web.setOnApplyWindowInsetsListener(new android.view.View.OnApplyWindowInsetsListener(){public android.view.WindowInsets onApplyWindowInsets(android.view.View v,android.view.WindowInsets insets){v.setPadding(insets.getSystemWindowInsetLeft(),insets.getSystemWindowInsetTop(),insets.getSystemWindowInsetRight(),insets.getSystemWindowInsetBottom());return insets.consumeSystemWindowInsets();}});web.requestApplyInsets();WebSettings s=web.getSettings();s.setJavaScriptEnabled(true);s.setDomStorageEnabled(true);s.setAllowFileAccess(false);s.setAllowContentAccess(false);s.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);s.setBuiltInZoomControls(false);s.setSupportMultipleWindows(false);s.setMediaPlaybackRequiresUserGesture(true);web.addJavascriptInterface(new Bridge(),"Android");
 web.setWebViewClient(new WebViewClient(){
 @Override public boolean shouldOverrideUrlLoading(WebView v,WebResourceRequest r){return true;}
 @Override public WebResourceResponse shouldInterceptRequest(WebView v,WebResourceRequest r){
  if(!ORIGIN.equals(r.getUrl().getScheme()+"://"+r.getUrl().getHost()))return new WebResourceResponse("text/plain","UTF-8",new ByteArrayInputStream(new byte[0]));
  String path=r.getUrl().getPath();if(path==null||path.equals("/"))path="/index.html";if(!path.matches("/(index\\.html|app\\.js|native-api\\.js|style\\.css)"))return new WebResourceResponse("text/plain","UTF-8",new ByteArrayInputStream(new byte[0]));
  try{return new WebResourceResponse(path.endsWith(".js")?"text/javascript":path.endsWith(".css")?"text/css":"text/html","UTF-8",getAssets().open("ui"+path));}catch(IOException e){return new WebResourceResponse("text/plain","UTF-8",new ByteArrayInputStream(new byte[0]));}
 }});web.loadUrl(ORIGIN+"/index.html");
 }
 private boolean allowed(URL u){if(!"https".equals(u.getProtocol())||u.getUserInfo()!=null||(u.getPort()!=-1&&u.getPort()!=443)||u.getRef()!=null)return false;String h=u.getHost(),p=u.getPath(),q=u.getQuery();
 if(h.equals("receive-smss.com"))return q==null&&(p.equals("/")||p.matches("/sms/[0-9]{7,15}/"));
 if(h.equals("esimplus.me"))return (q==null&&p.matches("/temporary-numbers(?:/[a-z-]+)?(?:/[0-9]{7,15})?/?"))||(p.matches("/api/sms-receiver/[0-9]{7,15}/sms")&&q!=null&&q.matches("perPage=(18|50)"));
 if(h.equals("receivesms.co"))return q==null&&(p.equals("/active-numbers")||p.matches("/[a-z]{2}-phone-number/[0-9]{1,8}/")||p.matches("/[a-z-]+-phone-numbers/[a-z]{2}/"));
 if(h.equals("sms-receive.net"))return (p.equals("/")&&q==null)||(p.equals("/get_sms_register.php")&&q!=null&&q.matches("phone=[0-9]{7,15}"));
 return h.equals("onlinesim.io")&&p.equals("/api/getFreeList")&&q!=null&&q.matches("lang=en&count=(20|50)(?:&country=[0-9]{1,3})?(?:&number=[0-9]{3,15})?");
 }
 private String download(String address)throws Exception{URL url=new URL(address);if(!allowed(url))throw new IOException("این آدرس در منابع پشتیبانی‌شده نیست");
 HttpsURLConnection c=(HttpsURLConnection)url.openConnection();c.setConnectTimeout(14000);c.setReadTimeout(16000);c.setInstanceFollowRedirects(false);c.setRequestProperty("User-Agent","FreeNumber/0.1 (public SMS viewer)");c.setRequestProperty("Accept","text/html,application/json");c.setRequestProperty("Cache-Control","no-cache");if(url.getHost().equals("sms-receive.net")){c.setRequestProperty("X-Alt-Data",String.valueOf(System.currentTimeMillis()/1000));c.setRequestProperty("Referer","https://sms-receive.net/");}
 try{int status=c.getResponseCode();if(status!=200)throw new IOException("منبع پاسخ قابل استفاده نداد (HTTP "+status+")");try(InputStream in=c.getInputStream();ByteArrayOutputStream out=new ByteArrayOutputStream()){byte[] buffer=new byte[8192];int n,total=0;while((n=in.read(buffer))!=-1){total+=n;if(total>8*1024*1024)throw new IOException("پاسخ منبع بیش از حد بزرگ است");out.write(buffer,0,n);}return new String(out.toByteArray(),"UTF-8");}}finally{c.disconnect();}}
 public final class Bridge {
 @JavascriptInterface public void request(final int id,final String address){workers.execute(new Runnable(){public void run(){JSONObject result=new JSONObject();try{result.put("text",download(address));}catch(Exception e){try{result.put("error",e instanceof SocketTimeoutException?"مهلت ارتباط با منبع تمام شد":(e.getMessage()==null?"دریافت از منبع ناموفق بود":e.getMessage()));}catch(Exception ignored){}}final String script="window.freeNumberReply("+id+","+result.toString()+");";runOnUiThread(new Runnable(){public void run(){if(!destroyed)web.evaluateJavascript(script,null);}});}});}
 @JavascriptInterface public void copy(final String text){runOnUiThread(new Runnable(){public void run(){android.content.ClipboardManager cb=(android.content.ClipboardManager)getSystemService(CLIPBOARD_SERVICE);cb.setPrimaryClip(ClipData.newPlainText("Free Number",text));}});}
 @JavascriptInterface public void quit(){runOnUiThread(new Runnable(){public void run(){finish();}});}
 }
 @Override public void onBackPressed(){finish();}
 @Override protected void onDestroy(){destroyed=true;workers.shutdownNow();if(web!=null){web.removeJavascriptInterface("Android");web.stopLoading();web.destroy();}super.onDestroy();}
}
