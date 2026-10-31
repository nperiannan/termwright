package com.github.nperiannan.termwright

import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import android.widget.Toast
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/** Quick settings tile: wakes the session chosen in the app ("Use for the Wake tile"). */
class WakeTileService : TileService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)

    override fun onStartListening() {
        val store = Store(this)
        val fav = store.sessions().firstOrNull { it.path == store.favourite }
        qsTile?.apply {
            state = if (fav != null) Tile.STATE_INACTIVE else Tile.STATE_UNAVAILABLE
            label = fav?.let { "Wake ${it.name}" } ?: getString(R.string.tile_label)
            updateTile()
        }
    }

    override fun onClick() {
        val store = Store(this)
        val all = store.sessions()
        val fav = all.firstOrNull { it.path == store.favourite } ?: return
        qsTile?.apply { state = Tile.STATE_ACTIVE; updateTile() }
        scope.launch {
            val text = when (val r = Core.wake(this@WakeTileService, store, fav, all)) {
                is WakeOutcome.Done -> r.text
                is WakeOutcome.Failed -> "Wake failed: ${r.text}"
                // these need an answer: the app asks
                is WakeOutcome.NeedLogin, is WakeOutcome.NewHostKey, is WakeOutcome.ChangedHostKey ->
                    "Open Termwright and wake ${fav.name} once there (a login or a key needs your answer)"
            }
            Toast.makeText(this@WakeTileService, text, Toast.LENGTH_LONG).show()
            qsTile?.apply { state = Tile.STATE_INACTIVE; updateTile() }
        }
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }
}
