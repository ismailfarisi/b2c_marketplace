package ae.marketplace.mobile.me

import ae.marketplace.mobile.model.Address
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.toFailure
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

data class AccountUiState(
    val profile: Profile? = null,
    val addresses: List<Address> = emptyList(),
    val preferences: List<NotificationPreference> = emptyList(),
    val loading: Boolean = false,
    val loaded: Boolean = false,
    /** The load failed; the sections show it with Retry. */
    val error: Failure? = null,
    /** Saving the profile. */
    val saving: Boolean = false,
    /** Bumped when a profile save succeeds, so the editor can close. */
    val profileSaved: Int = 0,
    /** A failed profile save; field errors go under each input. */
    val profileError: Failure? = null,
    /** An address being made default or removed. */
    val addressBusy: String? = null,
    val addressError: Failure? = null,
    /** Per channel: why a switch went back (422 not_optional, offline…). */
    val preferenceErrors: Map<String, Failure> = emptyMap(),
    /** familiar, balanced or surprise; null while unknown (the load is best effort). */
    val adventure: String? = null,
    val adventureError: Failure? = null,
)

/**
 * Profile, saved addresses and the marketing opt-out for the Me tab. Separate
 * from [MeViewModel] (classifieds and messages) so meal customers' account
 * screen does not wait on seller lists.
 */
class AccountViewModel(private val api: AccountApi) : ViewModel() {

    private val _state = MutableStateFlow(AccountUiState())
    val state: StateFlow<AccountUiState> = _state.asStateFlow()

    fun load(force: Boolean = false) {
        val s = _state.value
        if (s.loading || (s.loaded && !force)) return
        _state.update { it.copy(loading = true, error = null) }
        viewModelScope.launch {
            try {
                val profile = api.profile()
                val addresses = api.addresses().items
                // The switches are secondary: a failure here leaves them out
                // rather than hiding the profile.
                val prefs = runCatching { api.preferences().items }.getOrDefault(emptyList())
                val adventure = runCatching { api.mealPreferences().adventure }.getOrNull()
                _state.update {
                    it.copy(profile = profile, addresses = addresses, preferences = prefs, adventure = adventure, loading = false, loaded = true)
                }
            } catch (e: Throwable) {
                _state.update { it.copy(loading = false, error = e.toFailure()) }
            }
        }
    }

    fun saveProfile(displayName: String, email: String) {
        if (_state.value.saving) return
        _state.update { it.copy(saving = true, profileError = null) }
        viewModelScope.launch {
            try {
                val p = api.updateProfile(ProfileInput(displayName = displayName.trim(), email = email.trim()))
                _state.update { it.copy(profile = p, saving = false, profileSaved = it.profileSaved + 1) }
            } catch (e: Throwable) {
                _state.update { it.copy(saving = false, profileError = e.toFailure()) }
            }
        }
    }

    fun clearProfileError() = _state.update { it.copy(profileError = null) }

    fun makeDefault(id: String) = addressAction(id) {
        api.setDefaultAddress(id)
        _state.update { s -> s.copy(addresses = s.addresses.map { it.copy(isDefault = it.id == id) }.sortedByDescending { it.isDefault }) }
    }

    fun removeAddress(id: String) = addressAction(id) {
        api.deleteAddress(id)
        _state.update { s -> s.copy(addresses = s.addresses.filterNot { it.id == id }) }
        // Removing the default promotes another address on the server.
        val fresh = try {
            api.addresses().items
        } catch (e: CancellationException) {
            throw e
        } catch (_: Throwable) {
            null
        }
        fresh?.let { list -> _state.update { it.copy(addresses = list.sortedByDescending { a -> a.isDefault }) } }
    }

    private fun addressAction(id: String, block: suspend () -> Unit) {
        if (_state.value.addressBusy != null) return
        _state.update { it.copy(addressBusy = id, addressError = null) }
        viewModelScope.launch {
            try {
                block()
                _state.update { it.copy(addressBusy = null) }
            } catch (e: Throwable) {
                _state.update { it.copy(addressBusy = null, addressError = e.toFailure()) }
            }
        }
    }

    /** Optimistic; the switch goes back with the reason when the server refuses. */
    fun setPreference(pref: NotificationPreference, allowed: Boolean) {
        fun put(value: Boolean) = _state.update { s ->
            s.copy(preferences = s.preferences.map {
                if (it.category == pref.category && it.channel == pref.channel) it.copy(allowed = value) else it
            })
        }
        put(allowed)
        _state.update { it.copy(preferenceErrors = it.preferenceErrors - pref.channel) }
        viewModelScope.launch {
            try {
                api.setPreference(pref.category, pref.channel, allowed)
            } catch (e: Throwable) {
                val failure = e.toFailure()
                put(!allowed)
                _state.update { it.copy(preferenceErrors = it.preferenceErrors + (pref.channel to failure)) }
            }
        }
    }

    /** Changes the adventure setting; shows the new value at once and puts it back if saving fails. */
    fun setAdventure(adventure: String) {
        val before = _state.value.adventure
        _state.update { it.copy(adventure = adventure, adventureError = null) }
        viewModelScope.launch {
            try {
                api.setMealPreferences(adventure)
            } catch (e: Throwable) {
                _state.update { it.copy(adventure = before, adventureError = e.toFailure()) }
            }
        }
    }

    /** Drops the signed-out account's details. */
    fun forget() {
        _state.value = AccountUiState()
    }
}
