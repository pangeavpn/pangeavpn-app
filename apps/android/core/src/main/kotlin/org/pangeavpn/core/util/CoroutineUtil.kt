package org.pangeavpn.core.util

import kotlinx.coroutines.CancellationException

/** Like [runCatching] but rethrows [CancellationException] instead of wrapping it. */
suspend fun <T> runCatchingCancellable(block: suspend () -> T): Result<T> =
    try {
        Result.success(block())
    } catch (e: CancellationException) {
        throw e
    } catch (e: Throwable) {
        Result.failure(e)
    }
