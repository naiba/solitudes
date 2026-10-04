(function () {
  document.querySelectorAll('form[data-confirm]').forEach(form => form.addEventListener('submit', event => {
    if (!confirm(form.dataset.confirm)) event.preventDefault();
  }));
  async function request(url, payload, method = 'POST') {
    const response = await fetch(url, {
      method, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
      body: payload === undefined ? undefined : JSON.stringify(payload)
    });
    if (!response.ok) throw new Error(await response.text());
    return response.status === 204 || response.status === 201 ? null : response.json();
  }
  const register = document.getElementById('add-passkey');
  if (register) {
    register.addEventListener('click', async () => {
      if (register.disabled) return;
      register.disabled = true;
      try {
        if (!window.PublicKeyCredential?.parseCreationOptionsFromJSON) throw new Error(register.dataset.unsupportedWebauthn);
        const options = await request('/account/passkeys/begin', {});
        const credential = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey) });
        await request('/account/passkeys/finish', credential.toJSON());
        location.reload();
      } catch (error) { alert(error.message || String(error)); }
      finally { register.disabled = false; }
    });
    register.disabled = false;
  }
  document.querySelectorAll('[data-passkey-id]').forEach(button => {
    button.addEventListener('click', async () => {
      if (!confirm(button.dataset.confirmDelete)) return;
      button.disabled = true;
      try {
        await request('/account/passkeys/' + encodeURIComponent(button.dataset.passkeyId), undefined, 'DELETE');
        location.reload();
      } catch (error) { alert(error.message || String(error)); }
      finally { button.disabled = false; }
    });
    button.disabled = false;
  });
})();
