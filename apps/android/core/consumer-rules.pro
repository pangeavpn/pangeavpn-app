# The gomobile AAR is bound to Go by JNI on exact class and method names, and
# the callback interfaces are called from Go into our implementations.
-keep class go.** { *; }
-keep class org.pangeavpn.mobile.** { *; }
-keep class * implements org.pangeavpn.mobile.SocketProtector { *; }
-keep class * implements org.pangeavpn.mobile.StatusSink { *; }
-keep class * implements org.pangeavpn.mobile.SecretStore { *; }
-keep class * implements org.pangeavpn.mobile.NetworkKeyProvider { *; }

# Models cross the Go boundary as JSON, so R8 must not rename their fields.
-keepclassmembers class org.pangeavpn.core.model.** { *; }
