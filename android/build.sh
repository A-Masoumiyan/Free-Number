#!/usr/bin/env bash
set -euo pipefail
# Supply Android SDK platform 35, build-tools 35 and Eclipse ECJ 3.37+.
: "${ANDROID_JAR:?Set ANDROID_JAR}"
: "${ANDROID_BUILD_TOOLS:?Set ANDROID_BUILD_TOOLS}"
: "${ECJ_JAR:?Set ECJ_JAR}"
: "${FREE_NUMBER_KEYSTORE:?Set private signing keystore path}"
: "${FREE_NUMBER_STOREPASS:?Set private signing password}"
project_dir="$(cd "$(dirname "$0")/.." && pwd)"
build_dir="$project_dir/android/build"
mkdir -p "$build_dir/compiled" "$build_dir/classes" "$build_dir/dex" "$build_dir/assets/ui" "$project_dir/dist"
cp "$project_dir"/ui/* "$build_dir/assets/ui/"
"$ANDROID_BUILD_TOOLS/aapt2" compile --dir "$project_dir/android/res" -o "$build_dir/compiled/resources.zip"
"$ANDROID_BUILD_TOOLS/aapt2" link -o "$build_dir/base.apk" -I "$ANDROID_JAR" --manifest "$project_dir/android/AndroidManifest.xml" -A "$build_dir/assets" "$build_dir/compiled/resources.zip"
java -jar "$ECJ_JAR" -source 1.8 -target 1.8 -bootclasspath "$ANDROID_JAR" -d "$build_dir/classes" "$project_dir/android/src/org/freenumber/app/MainActivity.java"
mapfile -t class_files < <(find "$build_dir/classes" -name '*.class' -type f)
java -cp "$ANDROID_BUILD_TOOLS/lib/d8.jar" com.android.tools.r8.D8 --lib "$ANDROID_JAR" --min-api 26 --output "$build_dir/dex" "${class_files[@]}"
cp "$build_dir/base.apk" "$build_dir/unsigned.apk"
(cd "$build_dir/dex" && zip -q -u "$build_dir/unsigned.apk" classes.dex)
"$ANDROID_BUILD_TOOLS/zipalign" -f -p 4 "$build_dir/unsigned.apk" "$build_dir/aligned.apk"
java -jar "$ANDROID_BUILD_TOOLS/lib/apksigner.jar" sign --ks "$FREE_NUMBER_KEYSTORE" --ks-key-alias free-number --ks-pass env:FREE_NUMBER_STOREPASS --out "$project_dir/dist/Free-Number-0.1.0-Android.apk" "$build_dir/aligned.apk"
java -jar "$ANDROID_BUILD_TOOLS/lib/apksigner.jar" verify --verbose "$project_dir/dist/Free-Number-0.1.0-Android.apk"
