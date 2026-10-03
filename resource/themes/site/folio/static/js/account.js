(function () {
  async function request(url, payload, method = 'POST') {
    const response = await fetch(url, {
      method, credentials: 'same-origin', headers: { 'Content-Type': 'application/json' },
      body: payload === undefined ? undefined : JSON.stringify(payload)
    });
    if (!response.ok) throw new Error(await response.text());
    return response.status === 204 || response.status === 201 ? null : response.json();
  }
  const register = document.getElementById('add-passkey');
  if (register) register.addEventListener('click', async () => {
    try {
      if (!window.PublicKeyCredential?.parseCreationOptionsFromJSON) throw new Error(register.dataset.unsupportedWebauthn);
      const options = await request('/account/passkeys/begin', {});
      const credential = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(options.publicKey) });
      await request('/account/passkeys/finish', credential.toJSON());
      location.reload();
    } catch (error) { alert(error.message || String(error)); }
  });
  document.querySelectorAll('[data-passkey-id]').forEach(button => button.addEventListener('click', async () => {
    if (!confirm(button.dataset.confirmDelete)) return;
    try {
      await request('/account/passkeys/' + encodeURIComponent(button.dataset.passkeyId), undefined, 'DELETE');
      location.reload();
    } catch (error) { alert(error.message || String(error)); }
  }));
})();
