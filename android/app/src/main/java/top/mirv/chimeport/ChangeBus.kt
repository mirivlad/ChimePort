package top.mirv.chimeport

import kotlinx.coroutines.flow.MutableSharedFlow

/** In-process signal only; SQLite remains authoritative for activity and service. */
object ChangeBus {
    val changed = MutableSharedFlow<Unit>(extraBufferCapacity = 64)
    fun emit() { changed.tryEmit(Unit) }
}
