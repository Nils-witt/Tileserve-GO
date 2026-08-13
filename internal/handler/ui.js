    const TOKEN_KEY = 'tileserve_token';
    const USER_KEY = 'tileserve_username';

    const loginCard = document.getElementById('login-card');
    const app = document.getElementById('app');
    const loginForm = document.getElementById('login-form');
    const loginSubmit = document.getElementById('login-submit');
    const errorEl = document.getElementById('error');
    const appErrorEl = document.getElementById('app-error');
    const whoami = document.getElementById('whoami');
    const mapsBody = document.getElementById('maps-body');
    const emptyEl = document.getElementById('empty');
    const usersBody = document.getElementById('users-body');
    const usersErrorEl = document.getElementById('users-error');
    const tabMapsBtn = document.getElementById('tab-maps-btn');
    const tabUsersBtn = document.getElementById('tab-users-btn');
    const tabSyncBtn = document.getElementById('tab-sync-btn');
    const tabMaps = document.getElementById('tab-maps');
    const tabUsers = document.getElementById('tab-users');
    const tabSync = document.getElementById('tab-sync');
    const syncBody = document.getElementById('sync-body');
    const syncEmptyEl = document.getElementById('sync-empty');
    const syncErrorEl = document.getElementById('sync-error');
    const syncLogOverlay = document.getElementById('sync-log-overlay');
    const syncLogTitle = document.getElementById('sync-log-title');
    const syncLogBody = document.getElementById('sync-log-body');
    const syncLogEmptyEl = document.getElementById('sync-log-empty');
    const syncLogErrorEl = document.getElementById('sync-log-error');
    const syncLogClose = document.getElementById('sync-log-close');
    const syncLogRefresh = document.getElementById('sync-log-refresh');
    let syncLogRemoteId = null;
    const syncMapsOverlay = document.getElementById('sync-maps-overlay');
    const syncMapsTitle = document.getElementById('sync-maps-title');
    const syncMapsBody = document.getElementById('sync-maps-body');
    const syncMapsEmptyEl = document.getElementById('sync-maps-empty');
    const syncMapsErrorEl = document.getElementById('sync-maps-error');
    const syncMapsAllCb = document.getElementById('sync-maps-all');
    const syncMapsNewCb = document.getElementById('sync-maps-new');
    const syncMapsSaveBtn = document.getElementById('sync-maps-save');
    const syncMapsCloseBtn = document.getElementById('sync-maps-close');
    let syncMapsRemote = null;

    let isAdmin = false;
    let allUsers = [];

    function getToken() { return sessionStorage.getItem(TOKEN_KEY); }
    function setSession(token, username) {
      sessionStorage.setItem(TOKEN_KEY, token);
      sessionStorage.setItem(USER_KEY, username);
    }
    function clearSession() {
      sessionStorage.removeItem(TOKEN_KEY);
      sessionStorage.removeItem(USER_KEY);
    }

    async function api(path, options = {}) {
      const headers = Object.assign({}, options.headers, { Authorization: 'Bearer ' + getToken() });
      const res = await fetch(path, Object.assign({}, options, { headers }));
      if (res.status === 401) {
        clearSession();
        showLogin('Session expired, please sign in again.');
        throw new Error('unauthorized');
      }
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || ('request failed with status ' + res.status));
      }
      return res;
    }

    // generateKeyPair asks the server to generate a fresh RSA key pair
    // (nothing persisted server-side) — shared by the API-key modal and the
    // sync-remote form, since both need a caller-held private key plus a
    // public key to register somewhere.
    async function generateKeyPair() {
      const res = await api('/keys/generate', { method: 'POST' });
      return res.json();
    }

    function showLogin(message) {
      app.classList.add('hidden');
      loginCard.classList.remove('hidden');
      if (message) {
        errorEl.textContent = message;
        errorEl.classList.remove('hidden');
      } else {
        errorEl.classList.add('hidden');
      }
    }

    async function showApp() {
      loginCard.classList.add('hidden');
      app.classList.remove('hidden');
      whoami.textContent = sessionStorage.getItem(USER_KEY) || '';
      loadMaps();

      // GET /users itself doubles as the "am I admin" check: only admins are
      // allowed to call it, so a 403 here just means "hide the Users/Sync
      // tabs" (sync remote configuration is admin-only, same as user
      // management).
      try {
        const res = await api('/users');
        const users = await res.json();
        isAdmin = true;
        allUsers = users;
        tabUsersBtn.classList.remove('hidden');
        tabSyncBtn.classList.remove('hidden');
        renderUsers(users);
      } catch (err) {
        isAdmin = false;
        allUsers = [];
        tabUsersBtn.classList.add('hidden');
        tabSyncBtn.classList.add('hidden');
        showTab('maps');
      }
    }

    function showTab(name) {
      tabMaps.classList.toggle('hidden', name !== 'maps');
      tabUsers.classList.toggle('hidden', name !== 'users');
      tabSync.classList.toggle('hidden', name !== 'sync');
      tabMapsBtn.classList.toggle('active', name === 'maps');
      tabUsersBtn.classList.toggle('active', name === 'users');
      tabSyncBtn.classList.toggle('active', name === 'sync');
    }

    function fmtDate(iso) {
      const d = new Date(iso);
      return isNaN(d) ? iso : d.toLocaleString();
    }

    function appError(message) {
      if (!message) {
        appErrorEl.classList.add('hidden');
        return;
      }
      appErrorEl.textContent = message;
      appErrorEl.classList.remove('hidden');
    }

    async function loadMaps() {
      appError(null);
      try {
        const res = await api('/maps');
        const maps = await res.json();
        renderMaps(maps);
      } catch (err) {
        if (err.message !== 'unauthorized') appError(err.message);
      }
    }

    function renderMaps(maps) {
      mapsBody.innerHTML = '';
      emptyEl.classList.toggle('hidden', maps.length > 0);
      for (const m of maps) {
        mapsBody.appendChild(renderMapRow(m));
      }
    }

    function renderMapRow(m) {
      const tr = document.createElement('tr');

      const nameTd = document.createElement('td');
      nameTd.textContent = m.name;

      const versionTd = document.createElement('td');
      versionTd.textContent = m.currentVersion || '-';

      const visibleTd = document.createElement('td');
      visibleTd.className = 'checkbox-cell';
      const visibleCb = document.createElement('input');
      visibleCb.type = 'checkbox';
      visibleCb.checked = m.visibleToAll;
      visibleCb.title = 'Visible to all users';
      visibleCb.onchange = () => updateMapFlag(m, { visibleToAll: visibleCb.checked });
      visibleTd.appendChild(visibleCb);

      const anonymousTd = document.createElement('td');
      anonymousTd.className = 'checkbox-cell';
      const anonymousCb = document.createElement('input');
      anonymousCb.type = 'checkbox';
      anonymousCb.checked = m.anonymousAllowed;
      anonymousCb.title = 'Allow fetching tile files without signing in';
      anonymousCb.onchange = () => updateMapFlag(m, { anonymousAllowed: anonymousCb.checked });
      anonymousTd.appendChild(anonymousCb);

      const createdTd = document.createElement('td');
      createdTd.innerHTML = fmtDate(m.createdAt) + '<br><span class="muted">by ' + m.createdBy + '</span>';

      const updatedTd = document.createElement('td');
      updatedTd.innerHTML = fmtDate(m.updatedAt) + '<br><span class="muted">by ' + m.updatedBy + '</span>';

      const actionsTd = document.createElement('td');
      const actions = document.createElement('div');
      actions.className = 'actions';

      const editBtn = document.createElement('button');
      editBtn.className = 'secondary';
      editBtn.textContent = 'Edit';
      editBtn.onclick = () => editMap(m);

      const uploadBtn = document.createElement('button');
      uploadBtn.className = 'secondary';
      uploadBtn.textContent = 'Upload version';
      const fileInput = document.createElement('input');
      fileInput.type = 'file';
      fileInput.accept = '.zip,.tar,.tar.gz,.tgz';
      fileInput.className = 'hidden';
      fileInput.onchange = () => uploadVersion(m.uuid, fileInput.files[0]);
      uploadBtn.onclick = () => fileInput.click();

      const versionsBtn = document.createElement('button');
      versionsBtn.className = 'secondary';
      versionsBtn.textContent = 'Versions';
      versionsBtn.onclick = () => toggleVersions(m.uuid, versionsDiv, versionsBtn);

      const previewBtn = document.createElement('button');
      previewBtn.className = 'secondary';
      previewBtn.textContent = 'Preview';
      previewBtn.disabled = !m.currentVersion;
      if (!m.currentVersion) previewBtn.title = 'No uploaded version yet';
      previewBtn.onclick = () => openPreview(m);

      const deleteBtn = document.createElement('button');
      deleteBtn.className = 'danger';
      deleteBtn.textContent = 'Delete';
      deleteBtn.onclick = () => deleteMap(m.uuid);

      actions.append(editBtn, uploadBtn, fileInput, versionsBtn, previewBtn);

      const permBtn = document.createElement('button');
      permBtn.className = 'secondary';
      permBtn.textContent = 'Permissions';
      permBtn.onclick = () => openPermissions(m);
      actions.append(permBtn);

      const aliasBtn = document.createElement('button');
      aliasBtn.className = 'secondary';
      aliasBtn.textContent = 'Aliases';
      aliasBtn.onclick = () => openAliases(m);
      actions.append(aliasBtn);

      const geoBtn = document.createElement('button');
      geoBtn.className = 'secondary';
      geoBtn.textContent = 'Geo objects';
      geoBtn.disabled = !m.currentVersion;
      if (!m.currentVersion) geoBtn.title = 'No uploaded version yet';
      geoBtn.onclick = () => openGeoObjects(m);
      actions.append(geoBtn);

      actions.append(deleteBtn);

      const versionsDiv = document.createElement('div');
      versionsDiv.className = 'versions hidden';

      actionsTd.append(actions, versionsDiv);
      tr.append(nameTd, versionTd, visibleTd, anonymousTd, createdTd, updatedTd, actionsTd);
      return tr;
    }

    async function toggleVersions(id, container, button) {
      if (!container.classList.contains('hidden')) {
        container.classList.add('hidden');
        return;
      }
      appError(null);
      try {
        const res = await api('/maps/' + id + '/versions');
        const versions = await res.json();
        container.innerHTML = versions.length
          ? '<ul>' + versions.map(v => '<li>v' + v.version + ' — ' + fmtDate(v.createdAt) + ' by ' + v.createdBy + '</li>').join('') + '</ul>'
          : '<span class="muted">No versions uploaded yet.</span>';
        container.classList.remove('hidden');
      } catch (err) {
        if (err.message !== 'unauthorized') appError(err.message);
      }
    }

    async function createMap(name, currentVersion, visibleToAll, anonymousAllowed) {
      appError(null);
      try {
        await api('/maps', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name, currentVersion, visibleToAll, anonymousAllowed }),
        });
        await loadMaps();
      } catch (err) {
        if (err.message !== 'unauthorized') appError(err.message);
      }
    }

    // Toggles a single boolean flag (visibleToAll/anonymousAllowed) on m via
    // PUT, which replaces the whole map so the rest of its fields are sent
    // unchanged. Reloads the list afterwards either way, so a failed toggle
    // (e.g. 403) doesn't leave the checkbox showing a state the server
    // rejected.
    async function updateMapFlag(m, patch) {
      appError(null);
      try {
        await api('/maps/' + m.uuid, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(Object.assign({
            name: m.name,
            currentVersion: m.currentVersion,
            visibleToAll: m.visibleToAll,
            anonymousAllowed: m.anonymousAllowed,
          }, patch)),
        });
      } catch (err) {
        if (err.message !== 'unauthorized') appError(err.message);
      }
      await loadMaps();
    }

    async function editMap(m) {
      const name = prompt('Name', m.name);
      if (name === null) return;
      const currentVersion = prompt('Current version', m.currentVersion || '');
      if (currentVersion === null) return;
      appError(null);
      try {
        await api('/maps/' + m.uuid, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name, currentVersion, visibleToAll: m.visibleToAll, anonymousAllowed: m.anonymousAllowed }),
        });
        await loadMaps();
      } catch (err) {
        if (err.message !== 'unauthorized') appError(err.message);
      }
    }

    async function deleteMap(id) {
      if (!confirm('Delete this map and all its versions?')) return;
      appError(null);
      try {
        await api('/maps/' + id, { method: 'DELETE' });
        await loadMaps();
      } catch (err) {
        if (err.message !== 'unauthorized') appError(err.message);
      }
    }

    const previewOverlay = document.getElementById('preview-overlay');
    const previewTitle = document.getElementById('preview-title');
    const previewMapEl = document.getElementById('preview-map');
    const previewClose = document.getElementById('preview-close');
    let previewMap = null;

    async function openPreview(m) {
      if (!m.currentVersion) return;
      previewTitle.textContent = m.name + ' — v' + m.currentVersion;
      previewOverlay.classList.remove('hidden');

      const tileUrl = window.location.origin + '/maps/' + m.uuid + '/version/' + m.currentVersion +
        '/{z}/{x}/{y}.png?token=' + encodeURIComponent(getToken());

      // Centered on the world at zoom 1 as a fallback if bounds can't be
      // determined; otherwise centered on the tileset at a zoom level that
      // actually has tiles, so the preview shows something immediately.
      let center = [0, 0];
      let zoom = 1;
      try {
        const res = await api('/maps/' + m.uuid + '/version/' + m.currentVersion + '/bounds');
        const bounds = await res.json();
        center = [bounds.centerLng, bounds.centerLat];
        zoom = bounds.minZoom;
      } catch (err) {
        // fall back to the world view above
      }

      previewMap = new maplibregl.Map({
        container: previewMapEl,
        style: {
          version: 8,
          sources: {
            tiles: {
              type: 'raster',
              tiles: [tileUrl],
              tileSize: 256,
            },
          },
          layers: [{ id: 'tiles', type: 'raster', source: 'tiles' }],
        },
        center,
        zoom,
      });
      previewMap.addControl(new maplibregl.NavigationControl());
    }

    function closePreview() {
      previewOverlay.classList.add('hidden');
      if (previewMap) {
        previewMap.remove();
        previewMap = null;
      }
    }

    previewClose.addEventListener('click', closePreview);
    previewOverlay.addEventListener('click', (e) => {
      if (e.target === previewOverlay) closePreview();
    });

    const permOverlay = document.getElementById('perm-overlay');
    const permTitle = document.getElementById('perm-title');
    const permBody = document.getElementById('perm-body');
    const permEmptyEl = document.getElementById('perm-empty');
    const permErrorEl = document.getElementById('perm-error');
    const permClose = document.getElementById('perm-close');
    const permAddUser = document.getElementById('perm-add-user');
    const permAddView = document.getElementById('perm-add-view');
    const permAddEdit = document.getElementById('perm-add-edit');
    const permAddDelete = document.getElementById('perm-add-delete');
    const permAddBtn = document.getElementById('perm-add-btn');
    let permMapId = null;

    function permError(message) {
      if (!message) {
        permErrorEl.classList.add('hidden');
        return;
      }
      permErrorEl.textContent = message;
      permErrorEl.classList.remove('hidden');
    }

    async function openPermissions(m) {
      permMapId = m.uuid;
      permTitle.textContent = 'Permissions — ' + m.name;
      permError(null);
      permAddUser.innerHTML = allUsers.map(u => '<option value="' + u.username + '">' + u.username + '</option>').join('');
      permOverlay.classList.remove('hidden');
      await loadMapPermissions();
    }

    function closePermissions() {
      permOverlay.classList.add('hidden');
      permMapId = null;
    }

    async function loadMapPermissions() {
      permError(null);
      try {
        const res = await api('/maps/' + permMapId + '/permissions');
        renderMapPermissions(await res.json());
      } catch (err) {
        if (err.message !== 'unauthorized') permError(err.message);
      }
    }

    function renderMapPermissions(grants) {
      permBody.innerHTML = '';
      permEmptyEl.classList.toggle('hidden', grants.length > 0);
      for (const g of grants) {
        permBody.appendChild(renderMapPermissionRow(g));
      }
    }

    function renderMapPermissionRow(g) {
      const tr = document.createElement('tr');

      const userTd = document.createElement('td');
      userTd.textContent = g.username;

      const viewCb = document.createElement('input');
      viewCb.type = 'checkbox';
      viewCb.checked = g.canView;
      const editCb = document.createElement('input');
      editCb.type = 'checkbox';
      editCb.checked = g.canEdit;
      const deleteCb = document.createElement('input');
      deleteCb.type = 'checkbox';
      deleteCb.checked = g.canDelete;

      const saveBtn = document.createElement('button');
      saveBtn.className = 'secondary';
      saveBtn.textContent = 'Save';
      saveBtn.onclick = () => grantMapPermission(g.username, viewCb.checked, editCb.checked, deleteCb.checked);

      const revokeBtn = document.createElement('button');
      revokeBtn.className = 'danger';
      revokeBtn.textContent = 'Revoke';
      revokeBtn.onclick = () => revokeMapPermission(g.username);

      const actionsTd = document.createElement('td');
      const actions = document.createElement('div');
      actions.className = 'actions';
      actions.append(saveBtn, revokeBtn);
      actionsTd.append(actions);

      const viewTd = document.createElement('td');
      viewTd.className = 'checkbox-cell';
      viewTd.appendChild(viewCb);
      const editTd = document.createElement('td');
      editTd.className = 'checkbox-cell';
      editTd.appendChild(editCb);
      const deleteTd = document.createElement('td');
      deleteTd.className = 'checkbox-cell';
      deleteTd.appendChild(deleteCb);

      tr.append(userTd, viewTd, editTd, deleteTd, actionsTd);
      return tr;
    }

    async function grantMapPermission(username, canView, canEdit, canDelete) {
      permError(null);
      try {
        await api('/maps/' + permMapId + '/permissions/' + encodeURIComponent(username), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ canView, canEdit, canDelete }),
        });
        await loadMapPermissions();
      } catch (err) {
        if (err.message !== 'unauthorized') permError(err.message);
      }
    }

    async function revokeMapPermission(username) {
      permError(null);
      try {
        await api('/maps/' + permMapId + '/permissions/' + encodeURIComponent(username), { method: 'DELETE' });
        await loadMapPermissions();
      } catch (err) {
        if (err.message !== 'unauthorized') permError(err.message);
      }
    }

    permClose.addEventListener('click', closePermissions);
    permOverlay.addEventListener('click', (e) => {
      if (e.target === permOverlay) closePermissions();
    });
    permAddBtn.addEventListener('click', () => {
      if (!permAddUser.value) return;
      grantMapPermission(permAddUser.value, permAddView.checked, permAddEdit.checked, permAddDelete.checked);
    });

    const aliasOverlay = document.getElementById('alias-overlay');
    const aliasTitle = document.getElementById('alias-title');
    const aliasBody = document.getElementById('alias-body');
    const aliasEmptyEl = document.getElementById('alias-empty');
    const aliasErrorEl = document.getElementById('alias-error');
    const aliasClose = document.getElementById('alias-close');
    const aliasAddName = document.getElementById('alias-add-name');
    const aliasAddVersion = document.getElementById('alias-add-version');
    const aliasAddBtn = document.getElementById('alias-add-btn');
    let aliasMapId = null;

    function aliasError(message) {
      if (!message) {
        aliasErrorEl.classList.add('hidden');
        return;
      }
      aliasErrorEl.textContent = message;
      aliasErrorEl.classList.remove('hidden');
    }

    async function openAliases(m) {
      aliasMapId = m.uuid;
      aliasTitle.textContent = 'Aliases — ' + m.name;
      aliasError(null);
      aliasAddName.value = '';
      aliasAddVersion.value = '';
      aliasOverlay.classList.remove('hidden');
      await loadAliases();
    }

    function closeAliases() {
      aliasOverlay.classList.add('hidden');
      aliasMapId = null;
    }

    async function loadAliases() {
      aliasError(null);
      try {
        const res = await api('/maps/' + aliasMapId + '/aliases');
        renderAliases(await res.json());
      } catch (err) {
        if (err.message !== 'unauthorized') aliasError(err.message);
      }
    }

    function renderAliases(aliases) {
      aliasBody.innerHTML = '';
      aliasEmptyEl.classList.toggle('hidden', aliases.length > 0);
      for (const a of aliases) {
        aliasBody.appendChild(renderAliasRow(a));
      }
    }

    function renderAliasRow(a) {
      const tr = document.createElement('tr');

      const nameTd = document.createElement('td');
      nameTd.textContent = a.alias;

      const versionTd = document.createElement('td');
      versionTd.textContent = a.version;

      const updatedTd = document.createElement('td');
      updatedTd.innerHTML = fmtDate(a.updatedAt) + '<br><span class="muted">by ' + a.updatedBy + '</span>';

      const deleteBtn = document.createElement('button');
      deleteBtn.className = 'danger';
      deleteBtn.textContent = 'Delete';
      deleteBtn.onclick = () => deleteAlias(a.alias);

      const actionsTd = document.createElement('td');
      const actions = document.createElement('div');
      actions.className = 'actions';
      actions.append(deleteBtn);
      actionsTd.append(actions);

      tr.append(nameTd, versionTd, updatedTd, actionsTd);
      return tr;
    }

    async function saveAlias(alias, version) {
      aliasError(null);
      try {
        await api('/maps/' + aliasMapId + '/aliases/' + encodeURIComponent(alias), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ version }),
        });
        aliasAddName.value = '';
        aliasAddVersion.value = '';
        await loadAliases();
      } catch (err) {
        if (err.message !== 'unauthorized') aliasError(err.message);
      }
    }

    async function deleteAlias(alias) {
      aliasError(null);
      try {
        await api('/maps/' + aliasMapId + '/aliases/' + encodeURIComponent(alias), { method: 'DELETE' });
        await loadAliases();
      } catch (err) {
        if (err.message !== 'unauthorized') aliasError(err.message);
      }
    }

    aliasClose.addEventListener('click', closeAliases);
    aliasOverlay.addEventListener('click', (e) => {
      if (e.target === aliasOverlay) closeAliases();
    });
    aliasAddBtn.addEventListener('click', () => {
      if (!aliasAddName.value || !aliasAddVersion.value) return;
      saveAlias(aliasAddName.value.trim(), aliasAddVersion.value.trim());
    });

    const geoOverlay = document.getElementById('geo-overlay');
    const geoTitle = document.getElementById('geo-title');
    const geoVersionSelect = document.getElementById('geo-version');
    const geoBody = document.getElementById('geo-body');
    const geoEmptyEl = document.getElementById('geo-empty');
    const geoErrorEl = document.getElementById('geo-error');
    const geoClose = document.getElementById('geo-close');
    const geoCreateForm = document.getElementById('geo-create-form');
    let geoMapId = null;

    function geoError(message) {
      if (!message) {
        geoErrorEl.classList.add('hidden');
        return;
      }
      geoErrorEl.textContent = message;
      geoErrorEl.classList.remove('hidden');
    }

    // Geo objects are tied to one specific map version, so the modal opens
    // on the map's currentVersion and offers every uploaded version (from
    // GET /maps/{id}/versions) as an alternative via the select above the
    // table.
    async function openGeoObjects(m) {
      geoMapId = m.uuid;
      geoTitle.textContent = 'Geo objects — ' + m.name;
      geoError(null);
      geoOverlay.classList.remove('hidden');

      try {
        const res = await api('/maps/' + geoMapId + '/versions');
        const versions = await res.json();
        const versionValues = versions.map(v => v.version);
        if (!versionValues.includes(m.currentVersion)) versionValues.unshift(m.currentVersion);
        geoVersionSelect.innerHTML = versionValues.map(v => '<option value="' + v + '">v' + v + '</option>').join('');
        geoVersionSelect.value = m.currentVersion;
      } catch (err) {
        if (err.message !== 'unauthorized') geoError(err.message);
        geoVersionSelect.innerHTML = '<option value="' + m.currentVersion + '">v' + m.currentVersion + '</option>';
      }

      await loadGeoObjects();
    }

    function closeGeoObjects() {
      geoOverlay.classList.add('hidden');
      geoMapId = null;
    }

    function geoObjectPath(suffix) {
      return '/maps/' + geoMapId + '/version/' + encodeURIComponent(geoVersionSelect.value) + '/geo-objects' + suffix;
    }

    async function loadGeoObjects() {
      geoError(null);
      try {
        const res = await api(geoObjectPath(''));
        renderGeoObjects(await res.json());
      } catch (err) {
        if (err.message !== 'unauthorized') geoError(err.message);
      }
    }

    function renderGeoObjects(objs) {
      geoBody.innerHTML = '';
      geoEmptyEl.classList.toggle('hidden', objs.length > 0);
      for (const g of objs) {
        geoBody.appendChild(renderGeoObjectRow(g));
      }
    }

    function renderGeoObjectRow(g) {
      const tr = document.createElement('tr');

      const nameInput = document.createElement('input');
      nameInput.className = 'inline';
      nameInput.value = g.name;

      const externalInput = document.createElement('input');
      externalInput.className = 'inline';
      externalInput.value = g.externalId || '';

      const latInput = document.createElement('input');
      latInput.type = 'number';
      latInput.step = 'any';
      latInput.className = 'inline';
      latInput.value = g.latitude;

      const lonInput = document.createElement('input');
      lonInput.type = 'number';
      lonInput.step = 'any';
      lonInput.className = 'inline';
      lonInput.value = g.longitude;

      const streetInput = document.createElement('input');
      streetInput.className = 'inline';
      streetInput.value = g.street || '';

      const houseInput = document.createElement('input');
      houseInput.className = 'inline';
      houseInput.value = g.housenumber || '';

      const postcodeInput = document.createElement('input');
      postcodeInput.className = 'inline';
      postcodeInput.value = g.postcode || '';

      const saveBtn = document.createElement('button');
      saveBtn.className = 'secondary';
      saveBtn.textContent = 'Save';
      saveBtn.onclick = () => updateGeoObject(g.uuid, {
        name: nameInput.value,
        externalId: externalInput.value,
        latitude: parseFloat(latInput.value),
        longitude: parseFloat(lonInput.value),
        street: streetInput.value,
        housenumber: houseInput.value,
        postcode: postcodeInput.value,
      });

      const deleteBtn = document.createElement('button');
      deleteBtn.className = 'danger';
      deleteBtn.textContent = 'Delete';
      deleteBtn.onclick = () => deleteGeoObject(g.uuid);

      const actionsTd = document.createElement('td');
      const actions = document.createElement('div');
      actions.className = 'actions';
      actions.append(saveBtn, deleteBtn);
      actionsTd.append(actions);

      const nameTd = document.createElement('td');
      nameTd.appendChild(nameInput);
      const externalTd = document.createElement('td');
      externalTd.appendChild(externalInput);
      const latTd = document.createElement('td');
      latTd.appendChild(latInput);
      const lonTd = document.createElement('td');
      lonTd.appendChild(lonInput);
      const streetTd = document.createElement('td');
      streetTd.appendChild(streetInput);
      const houseTd = document.createElement('td');
      houseTd.appendChild(houseInput);
      const postcodeTd = document.createElement('td');
      postcodeTd.appendChild(postcodeInput);

      tr.append(nameTd, externalTd, latTd, lonTd, streetTd, houseTd, postcodeTd, actionsTd);
      return tr;
    }

    async function createGeoObject(payload) {
      geoError(null);
      try {
        await api(geoObjectPath(''), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        await loadGeoObjects();
      } catch (err) {
        if (err.message !== 'unauthorized') geoError(err.message);
      }
    }

    async function updateGeoObject(id, payload) {
      geoError(null);
      try {
        await api(geoObjectPath('/' + id), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        await loadGeoObjects();
      } catch (err) {
        if (err.message !== 'unauthorized') geoError(err.message);
      }
    }

    async function deleteGeoObject(id) {
      if (!confirm('Delete this geo object?')) return;
      geoError(null);
      try {
        await api(geoObjectPath('/' + id), { method: 'DELETE' });
        await loadGeoObjects();
      } catch (err) {
        if (err.message !== 'unauthorized') geoError(err.message);
      }
    }

    geoClose.addEventListener('click', closeGeoObjects);
    geoOverlay.addEventListener('click', (e) => {
      if (e.target === geoOverlay) closeGeoObjects();
    });
    geoVersionSelect.addEventListener('change', loadGeoObjects);
    geoCreateForm.addEventListener('submit', (e) => {
      e.preventDefault();
      const payload = {
        name: document.getElementById('geo-create-name').value,
        externalId: document.getElementById('geo-create-external').value,
        latitude: parseFloat(document.getElementById('geo-create-lat').value),
        longitude: parseFloat(document.getElementById('geo-create-lon').value),
        street: document.getElementById('geo-create-street').value,
        housenumber: document.getElementById('geo-create-housenumber').value,
        postcode: document.getElementById('geo-create-postcode').value,
      };
      createGeoObject(payload).then(() => {
        e.target.reset();
      });
    });

    async function uploadVersion(id, file) {
      if (!file) return;
      appError(null);
      try {
        await api('/maps/' + id + '/upload', {
          method: 'POST',
          headers: { 'Content-Type': file.type || 'application/octet-stream' },
          body: file,
        });
        await loadMaps();
      } catch (err) {
        if (err.message !== 'unauthorized') appError(err.message);
      }
    }

    function usersError(message) {
      if (!message) {
        usersErrorEl.classList.add('hidden');
        return;
      }
      usersErrorEl.textContent = message;
      usersErrorEl.classList.remove('hidden');
    }

    async function loadUsers() {
      usersError(null);
      try {
        const res = await api('/users');
        allUsers = await res.json();
        renderUsers(allUsers);
      } catch (err) {
        if (err.message !== 'unauthorized') usersError(err.message);
      }
    }

    function renderUsers(users) {
      usersBody.innerHTML = '';
      for (const u of users) {
        usersBody.appendChild(renderUserRow(u));
      }
    }

    function renderUserRow(u) {
      const tr = document.createElement('tr');
      const self = u.username === sessionStorage.getItem(USER_KEY);

      const usernameTd = document.createElement('td');
      usernameTd.textContent = u.username + (self ? ' (you)' : '');

      const cnInput = document.createElement('input');
      cnInput.className = 'inline';
      cnInput.value = u.cn || '';

      const createCb = document.createElement('input');
      createCb.type = 'checkbox';
      createCb.checked = u.canCreate;
      const editCb = document.createElement('input');
      editCb.type = 'checkbox';
      editCb.checked = u.canEdit;
      const deleteCb = document.createElement('input');
      deleteCb.type = 'checkbox';
      deleteCb.checked = u.canDelete;
      const adminCb = document.createElement('input');
      adminCb.type = 'checkbox';
      adminCb.checked = u.isAdmin;

      const passwordInput = document.createElement('input');
      passwordInput.type = 'password';
      passwordInput.className = 'inline';
      passwordInput.placeholder = 'leave blank to keep';

      const createdTd = document.createElement('td');
      createdTd.textContent = fmtDate(u.createdAt);

      const saveBtn = document.createElement('button');
      saveBtn.className = 'secondary';
      saveBtn.textContent = 'Save';
      saveBtn.onclick = () => updateUser(u.username, {
        cn: cnInput.value,
        canCreate: createCb.checked,
        canEdit: editCb.checked,
        canDelete: deleteCb.checked,
        isAdmin: adminCb.checked,
        password: passwordInput.value,
      });

      const apikeyBtn = document.createElement('button');
      apikeyBtn.className = 'secondary';
      apikeyBtn.textContent = 'API keys';
      apikeyBtn.onclick = () => openAPIKeys(u.username);

      const deleteBtn = document.createElement('button');
      deleteBtn.className = 'danger';
      deleteBtn.textContent = 'Delete';
      deleteBtn.disabled = self;
      if (self) deleteBtn.title = "You can't delete your own account";
      deleteBtn.onclick = () => deleteUser(u.username);

      const actionsTd = document.createElement('td');
      const actions = document.createElement('div');
      actions.className = 'actions';
      actions.append(saveBtn, apikeyBtn, deleteBtn);
      actionsTd.append(actions);

      const passwordTd = document.createElement('td');
      passwordTd.appendChild(passwordInput);

      const cnTd = document.createElement('td');
      cnTd.appendChild(cnInput);

      const createTd = document.createElement('td');
      createTd.className = 'checkbox-cell';
      createTd.appendChild(createCb);
      const editTd = document.createElement('td');
      editTd.className = 'checkbox-cell';
      editTd.appendChild(editCb);
      const deleteTd = document.createElement('td');
      deleteTd.className = 'checkbox-cell';
      deleteTd.appendChild(deleteCb);
      const adminTd = document.createElement('td');
      adminTd.className = 'checkbox-cell';
      adminTd.appendChild(adminCb);

      tr.append(usernameTd, cnTd, createTd, editTd, deleteTd, adminTd, passwordTd, createdTd, actionsTd);
      return tr;
    }

    async function createUser(payload) {
      usersError(null);
      try {
        await api('/users', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        await loadUsers();
      } catch (err) {
        if (err.message !== 'unauthorized') usersError(err.message);
      }
    }

    async function updateUser(username, payload) {
      usersError(null);
      try {
        await api('/users/' + encodeURIComponent(username), {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        await loadUsers();
      } catch (err) {
        if (err.message !== 'unauthorized') usersError(err.message);
      }
    }

    async function deleteUser(username) {
      if (!confirm('Delete user "' + username + '"?')) return;
      usersError(null);
      try {
        await api('/users/' + encodeURIComponent(username), { method: 'DELETE' });
        await loadUsers();
      } catch (err) {
        if (err.message !== 'unauthorized') usersError(err.message);
      }
    }

    const apikeyOverlay = document.getElementById('apikey-overlay');
    const apikeyTitle = document.getElementById('apikey-title');
    const apikeyBody = document.getElementById('apikey-body');
    const apikeyEmptyEl = document.getElementById('apikey-empty');
    const apikeyErrorEl = document.getElementById('apikey-error');
    const apikeyClose = document.getElementById('apikey-close');
    const apikeyAddName = document.getElementById('apikey-add-name');
    const apikeyAddPubkey = document.getElementById('apikey-add-pubkey');
    const apikeyGenerateBtn = document.getElementById('apikey-generate-btn');
    const apikeyAddBtn = document.getElementById('apikey-add-btn');
    let apikeyUsername = null;

    function apikeyError(message) {
      if (!message) {
        apikeyErrorEl.classList.add('hidden');
        return;
      }
      apikeyErrorEl.textContent = message;
      apikeyErrorEl.classList.remove('hidden');
    }

    async function openAPIKeys(username) {
      apikeyUsername = username;
      apikeyTitle.textContent = 'API keys — ' + username;
      apikeyError(null);
      apikeyAddName.value = '';
      apikeyAddPubkey.value = '';
      apikeyOverlay.classList.remove('hidden');
      await loadAPIKeys();
    }

    function closeAPIKeys() {
      apikeyOverlay.classList.add('hidden');
      apikeyUsername = null;
    }

    async function loadAPIKeys() {
      apikeyError(null);
      try {
        const res = await api('/users/' + encodeURIComponent(apikeyUsername) + '/api-keys');
        renderAPIKeys(await res.json());
      } catch (err) {
        if (err.message !== 'unauthorized') apikeyError(err.message);
      }
    }

    function renderAPIKeys(keys) {
      apikeyBody.innerHTML = '';
      apikeyEmptyEl.classList.toggle('hidden', keys.length > 0);
      for (const k of keys) {
        apikeyBody.appendChild(renderAPIKeyRow(k));
      }
    }

    function renderAPIKeyRow(k) {
      const tr = document.createElement('tr');

      const nameTd = document.createElement('td');
      nameTd.textContent = k.name || '-';

      const createdTd = document.createElement('td');
      createdTd.innerHTML = fmtDate(k.createdAt) + '<br><span class="muted">by ' + k.createdBy + '</span>';

      const lastUsedTd = document.createElement('td');
      lastUsedTd.textContent = k.lastUsedAt ? fmtDate(k.lastUsedAt) : 'never';

      const revokeBtn = document.createElement('button');
      revokeBtn.className = 'danger';
      revokeBtn.textContent = 'Revoke';
      revokeBtn.onclick = () => revokeAPIKey(k.id);

      const actionsTd = document.createElement('td');
      const actions = document.createElement('div');
      actions.className = 'actions';
      actions.append(revokeBtn);
      actionsTd.append(actions);

      tr.append(nameTd, createdTd, lastUsedTd, actionsTd);
      return tr;
    }

    async function createAPIKey(name, publicKeyPem) {
      apikeyError(null);
      try {
        const res = await api('/users/' + encodeURIComponent(apikeyUsername) + '/api-keys', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name, publicKeyPem }),
        });
        const created = await res.json();
        apikeyAddName.value = '';
        apikeyAddPubkey.value = '';
        await loadAPIKeys();
        // Nothing secret comes back here — the server never sees a private
        // key — but the caller still needs to know which id to use as the
        // JWT `kid` when signing tokens for this key.
        alert('API key registered. Key ID (use as the JWT "kid" when signing tokens): ' + created.id);
      } catch (err) {
        if (err.message !== 'unauthorized') apikeyError(err.message);
      }
    }

    async function revokeAPIKey(id) {
      if (!confirm('Revoke this API key? Anything still using it will lose access immediately.')) return;
      apikeyError(null);
      try {
        await api('/users/' + encodeURIComponent(apikeyUsername) + '/api-keys/' + id, { method: 'DELETE' });
        await loadAPIKeys();
      } catch (err) {
        if (err.message !== 'unauthorized') apikeyError(err.message);
      }
    }

    apikeyClose.addEventListener('click', closeAPIKeys);
    apikeyOverlay.addEventListener('click', (e) => {
      if (e.target === apikeyOverlay) closeAPIKeys();
    });
    apikeyGenerateBtn.addEventListener('click', async () => {
      apikeyError(null);
      try {
        const kp = await generateKeyPair();
        // The private key is only ever available here, once — a blocking
        // prompt (pre-filled, selectable) is the simplest way to give the
        // admin a chance to copy it before it's gone for good.
        prompt('Save this private key now — it will not be shown again:', kp.privateKeyPem);
        apikeyAddPubkey.value = kp.publicKeyPem;
      } catch (err) {
        if (err.message !== 'unauthorized') apikeyError(err.message);
      }
    });
    apikeyAddBtn.addEventListener('click', () => {
      if (!apikeyAddName.value.trim() || !apikeyAddPubkey.value.trim()) return;
      createAPIKey(apikeyAddName.value.trim(), apikeyAddPubkey.value.trim());
    });

    function syncError(message) {
      if (!message) {
        syncErrorEl.classList.add('hidden');
        return;
      }
      syncErrorEl.textContent = message;
      syncErrorEl.classList.remove('hidden');
    }

    async function loadSyncRemotes() {
      syncError(null);
      try {
        const res = await api('/sync/remotes');
        renderSyncRemotes(await res.json());
      } catch (err) {
        if (err.message !== 'unauthorized') syncError(err.message);
      }
    }

    function renderSyncRemotes(remotes) {
      syncBody.innerHTML = '';
      syncEmptyEl.classList.toggle('hidden', remotes.length > 0);
      for (const r of remotes) {
        syncBody.appendChild(renderSyncRemoteRow(r));
      }
    }

    // fmtSyncStatus renders a remote's last sync outcome: never-synced,
    // ok, or error (with the error message truncated inline — the full
    // text is still available via the title tooltip).
    function fmtSyncStatus(r) {
      if (!r.lastSyncAt) return '<span class="muted">never synced</span>';
      const status = r.lastSyncStatus === 'error'
        ? '<span style="color:#dc2626">error</span>'
        : (r.lastSyncStatus || '-');
      let html = status + '<br><span class="muted">' + fmtDate(r.lastSyncAt) + '</span>';
      if (r.lastSyncError) {
        const short = r.lastSyncError.length > 60 ? r.lastSyncError.slice(0, 60) + '…' : r.lastSyncError;
        const esc = r.lastSyncError.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
        html += '<br><span class="muted" title="' + esc + '">' + short + '</span>';
      }
      return html;
    }

    function renderSyncRemoteRow(r) {
      const tr = document.createElement('tr');

      const nameTd = document.createElement('td');
      nameTd.textContent = r.name;

      const urlTd = document.createElement('td');
      urlTd.textContent = r.baseUrl;

      const intervalTd = document.createElement('td');
      intervalTd.textContent = r.pollIntervalSec + 's';

      const scopeTd = document.createElement('td');
      scopeTd.textContent = r.syncAllMaps ? 'All maps' : 'Selected maps';

      const enabledTd = document.createElement('td');
      enabledTd.className = 'checkbox-cell';
      const enabledCb = document.createElement('input');
      enabledCb.type = 'checkbox';
      enabledCb.checked = r.enabled;
      enabledCb.onchange = () => updateSyncRemote(r, { enabled: enabledCb.checked });
      enabledTd.appendChild(enabledCb);

      const statusTd = document.createElement('td');
      statusTd.innerHTML = fmtSyncStatus(r);

      const editBtn = document.createElement('button');
      editBtn.className = 'secondary';
      editBtn.textContent = 'Edit';
      editBtn.onclick = () => editSyncRemote(r);

      const mapsBtn = document.createElement('button');
      mapsBtn.className = 'secondary';
      mapsBtn.textContent = 'Select maps';
      mapsBtn.onclick = () => openSyncMapsModal(r);

      const triggerBtn = document.createElement('button');
      triggerBtn.className = 'secondary';
      triggerBtn.textContent = 'Sync now';
      triggerBtn.onclick = () => triggerSyncRemote(r.id);

      const logBtn = document.createElement('button');
      logBtn.className = 'secondary';
      logBtn.textContent = 'Logs';
      logBtn.onclick = () => openSyncLog(r);

      const deleteBtn = document.createElement('button');
      deleteBtn.className = 'danger';
      deleteBtn.textContent = 'Delete';
      deleteBtn.onclick = () => deleteSyncRemote(r.id);

      const actionsTd = document.createElement('td');
      const actions = document.createElement('div');
      actions.className = 'actions';
      actions.append(editBtn, mapsBtn, triggerBtn, logBtn, deleteBtn);
      actionsTd.append(actions);

      tr.append(nameTd, urlTd, intervalTd, scopeTd, enabledTd, statusTd, actionsTd);
      return tr;
    }

    async function createSyncRemote(payload) {
      syncError(null);
      try {
        await api('/sync/remotes', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        await loadSyncRemotes();
      } catch (err) {
        if (err.message !== 'unauthorized') syncError(err.message);
      }
    }

    // updateSyncRemote PUTs the full remote configuration, merging patch
    // over r's current fields — mirrors updateMapFlag's pattern for
    // toggling a single boolean via a full-replace endpoint. remoteApiKeyId
    // isn't secret, so it's always resent from r; privateKeyPem is
    // deliberately omitted: PUT /sync/remotes/{id} treats a missing/empty
    // privateKeyPem as "keep the current one" (see store.UpdateSyncRemote),
    // since GET /sync/remotes never echoes it back for this code to resend.
    async function updateSyncRemote(r, patch) {
      syncError(null);
      try {
        await api('/sync/remotes/' + r.id, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(Object.assign({
            name: r.name,
            baseUrl: r.baseUrl,
            remoteApiKeyId: r.remoteApiKeyId,
            pollIntervalSec: r.pollIntervalSec,
            enabled: r.enabled,
            syncAllMaps: r.syncAllMaps,
            syncNewMaps: r.syncNewMaps,
          }, patch)),
        });
      } catch (err) {
        if (err.message !== 'unauthorized') syncError(err.message);
      }
      await loadSyncRemotes();
    }

    // editSyncRemote uses the existing sequential-prompt() pattern for the
    // short scalar fields, but a multi-line PEM private key doesn't fit a
    // single-line prompt() well — so instead of asking the admin to paste
    // one, this offers to generate a fresh key pair (same as the create
    // form) and only prompts for the new remote key id once it's been
    // registered on the remote.
    async function editSyncRemote(r) {
      const name = prompt('Name', r.name);
      if (name === null) return;
      const baseUrl = prompt('Base URL', r.baseUrl);
      if (baseUrl === null) return;

      let remoteApiKeyId = r.remoteApiKeyId;
      let privateKeyPem = '';

      if (confirm('Generate a new key pair for this remote? (Cancel to keep the current one)')) {
        syncError(null);
        try {
          const kp = await generateKeyPair();
          prompt('Public key — register this as a new API key on the remote, then note the resulting key ID:', kp.publicKeyPem);
          privateKeyPem = kp.privateKeyPem;
        } catch (err) {
          if (err.message !== 'unauthorized') syncError(err.message);
          return;
        }

        const newId = prompt('New remote API key ID (from the remote\'s response)', remoteApiKeyId);
        if (newId === null) return;
        remoteApiKeyId = newId;
      } else {
        const newId = prompt('Remote API key ID', remoteApiKeyId);
        if (newId === null) return;
        remoteApiKeyId = newId;
      }

      const pollIntervalSec = prompt('Poll interval (seconds)', r.pollIntervalSec);
      if (pollIntervalSec === null) return;
      syncError(null);
      try {
        await api('/sync/remotes/' + r.id, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            name,
            baseUrl,
            remoteApiKeyId,
            privateKeyPem,
            pollIntervalSec: parseInt(pollIntervalSec, 10),
            enabled: r.enabled,
            syncAllMaps: r.syncAllMaps,
            syncNewMaps: r.syncNewMaps,
            // selectedMapUuids deliberately omitted, same reasoning as
            // updateSyncRemote: it's edited via the "Select maps" modal,
            // not this prompt-based flow, and omitting it here leaves the
            // saved selection untouched.
          }),
        });
        await loadSyncRemotes();
      } catch (err) {
        if (err.message !== 'unauthorized') syncError(err.message);
      }
    }

    async function triggerSyncRemote(id) {
      syncError(null);
      try {
        await api('/sync/remotes/' + id + '/trigger', { method: 'POST' });
      } catch (err) {
        if (err.message !== 'unauthorized') syncError(err.message);
      }
    }

    async function deleteSyncRemote(id) {
      if (!confirm('Remove this sync remote? Already-mirrored maps keep their local data.')) return;
      syncError(null);
      try {
        await api('/sync/remotes/' + id, { method: 'DELETE' });
        await loadSyncRemotes();
      } catch (err) {
        if (err.message !== 'unauthorized') syncError(err.message);
      }
    }

    function syncLogError(message) {
      if (!message) {
        syncLogErrorEl.classList.add('hidden');
        return;
      }
      syncLogErrorEl.textContent = message;
      syncLogErrorEl.classList.remove('hidden');
    }

    async function openSyncLog(r) {
      syncLogRemoteId = r.id;
      syncLogTitle.textContent = 'Sync log — ' + r.name;
      syncLogOverlay.classList.remove('hidden');
      await loadSyncLog();
    }

    function closeSyncLog() {
      syncLogOverlay.classList.add('hidden');
      syncLogRemoteId = null;
    }

    async function loadSyncLog() {
      syncLogError(null);
      try {
        const res = await api('/sync/remotes/' + syncLogRemoteId + '/logs');
        renderSyncLog(await res.json());
      } catch (err) {
        if (err.message !== 'unauthorized') syncLogError(err.message);
      }
    }

    // renderSyncLog shows the newest entry first, since that's the one an
    // admin checking on a sync is almost always looking for. Error-level
    // entries are highlighted so a failure stands out among routine lines.
    function renderSyncLog(entries) {
      syncLogEmptyEl.classList.toggle('hidden', entries.length > 0);
      syncLogBody.innerHTML = entries
        .slice()
        .reverse()
        .map((e) => {
          const line = '[' + fmtDate(e.time) + '] ' + e.message;
          const esc = line.replace(/&/g, '&amp;').replace(/</g, '&lt;');
          return e.level === 'error' ? '<span style="color:#dc2626">' + esc + '</span>' : esc;
        })
        .join('\n');
    }

    syncLogClose.addEventListener('click', closeSyncLog);
    syncLogRefresh.addEventListener('click', loadSyncLog);
    syncLogOverlay.addEventListener('click', (e) => {
      if (e.target === syncLogOverlay) closeSyncLog();
    });

    function syncMapsError(message) {
      if (!message) {
        syncMapsErrorEl.classList.add('hidden');
        return;
      }
      syncMapsErrorEl.textContent = message;
      syncMapsErrorEl.classList.remove('hidden');
    }

    async function openSyncMapsModal(r) {
      syncMapsRemote = r;
      syncMapsTitle.textContent = 'Select maps — ' + r.name;
      syncMapsAllCb.checked = r.syncAllMaps;
      syncMapsNewCb.checked = r.syncNewMaps;
      syncMapsNewCb.disabled = r.syncAllMaps;
      syncMapsOverlay.classList.remove('hidden');
      await loadSyncMapsPicker();
    }

    function closeSyncMapsModal() {
      syncMapsOverlay.classList.add('hidden');
      syncMapsRemote = null;
      syncMapsBody.innerHTML = '';
    }

    // loadSyncMapsPicker fetches both the remote's live map list (what can
    // be selected) and this remote's already-saved selection (what's
    // currently checked), so renderSyncMapsPicker can pre-check the right
    // boxes.
    async function loadSyncMapsPicker() {
      syncMapsError(null);
      syncMapsBody.innerHTML = '';
      try {
        const [remoteRes, selectedRes] = await Promise.all([
          api('/sync/remotes/' + syncMapsRemote.id + '/remote-maps'),
          api('/sync/remotes/' + syncMapsRemote.id + '/selected-maps'),
        ]);
        const remoteMaps = await remoteRes.json();
        const selectedIds = new Set(await selectedRes.json());
        renderSyncMapsPicker(remoteMaps, selectedIds);
      } catch (err) {
        if (err.message !== 'unauthorized') syncMapsError(err.message);
      }
    }

    function renderSyncMapsPicker(remoteMaps, selectedIds) {
      syncMapsEmptyEl.classList.toggle('hidden', remoteMaps.length > 0);
      syncMapsBody.innerHTML = '';
      for (const m of remoteMaps) {
        const tr = document.createElement('tr');

        const cbTd = document.createElement('td');
        cbTd.className = 'checkbox-cell';
        const cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.checked = selectedIds.has(m.uuid);
        cb.dataset.mapUuid = m.uuid;
        cbTd.appendChild(cb);

        const nameTd = document.createElement('td');
        nameTd.textContent = m.name;

        const idTd = document.createElement('td');
        idTd.textContent = m.uuid;
        idTd.className = 'muted';

        tr.append(cbTd, nameTd, idTd);
        syncMapsBody.appendChild(tr);
      }
    }

    // saveSyncMapsSelection PUTs the full remote configuration (mirroring
    // updateSyncRemote's full-replace pattern), setting selectedMapUuids
    // explicitly — the one place in the UI that intentionally overwrites
    // the saved selection.
    async function saveSyncMapsSelection() {
      syncMapsError(null);
      const r = syncMapsRemote;
      const selectedMapUuids = Array.from(syncMapsBody.querySelectorAll('input[type=checkbox]:checked'))
        .map((cb) => cb.dataset.mapUuid);
      try {
        await api('/sync/remotes/' + r.id, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            name: r.name,
            baseUrl: r.baseUrl,
            remoteApiKeyId: r.remoteApiKeyId,
            pollIntervalSec: r.pollIntervalSec,
            enabled: r.enabled,
            syncAllMaps: syncMapsAllCb.checked,
            syncNewMaps: syncMapsNewCb.checked,
            selectedMapUuids,
          }),
        });
        closeSyncMapsModal();
        await loadSyncRemotes();
      } catch (err) {
        if (err.message !== 'unauthorized') syncMapsError(err.message);
      }
    }

    syncMapsAllCb.addEventListener('change', () => {
      syncMapsNewCb.disabled = syncMapsAllCb.checked;
    });
    syncMapsSaveBtn.addEventListener('click', saveSyncMapsSelection);
    syncMapsCloseBtn.addEventListener('click', closeSyncMapsModal);
    syncMapsOverlay.addEventListener('click', (e) => {
      if (e.target === syncMapsOverlay) closeSyncMapsModal();
    });

    document.getElementById('sync-generate-btn').addEventListener('click', async () => {
      syncError(null);
      try {
        const kp = await generateKeyPair();
        document.getElementById('sync-private-key-pem').value = kp.privateKeyPem;
        document.getElementById('sync-public-key-pem').value = kp.publicKeyPem;
      } catch (err) {
        if (err.message !== 'unauthorized') syncError(err.message);
      }
    });

    document.getElementById('create-sync-form').addEventListener('submit', (e) => {
      e.preventDefault();
      const payload = {
        name: document.getElementById('sync-name').value,
        baseUrl: document.getElementById('sync-base-url').value,
        remoteApiKeyId: document.getElementById('sync-remote-key-id').value,
        privateKeyPem: document.getElementById('sync-private-key-pem').value,
        pollIntervalSec: parseInt(document.getElementById('sync-interval').value, 10),
        enabled: document.getElementById('sync-enabled').checked,
        // A freshly registered remote starts in "sync everything" mode,
        // matching this feature's behavior before selective sync existed;
        // use the "Select maps" button afterwards to narrow it down.
        syncAllMaps: true,
        syncNewMaps: false,
      };
      createSyncRemote(payload).then(() => {
        e.target.reset();
        document.getElementById('sync-public-key-pem').value = '';
        document.getElementById('sync-private-key-pem').value = '';
        document.getElementById('sync-interval').value = 300;
        document.getElementById('sync-enabled').checked = true;
      });
    });

    tabSyncBtn.addEventListener('click', () => { showTab('sync'); loadSyncRemotes(); });

    loginForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      errorEl.classList.add('hidden');
      loginSubmit.disabled = true;
      loginSubmit.textContent = 'Signing in…';
      try {
        const username = document.getElementById('username').value;
        const password = document.getElementById('password').value;
        const res = await fetch('/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username, password }),
        });
        if (!res.ok) {
          throw new Error(res.status === 401 ? 'Invalid credentials' : 'Login failed');
        }
        const data = await res.json();
        setSession(data.token, username);
        showApp();
      } catch (err) {
        errorEl.textContent = err.message;
        errorEl.classList.remove('hidden');
      } finally {
        loginSubmit.disabled = false;
        loginSubmit.textContent = 'Sign in';
      }
    });

    document.getElementById('logout').addEventListener('click', () => {
      clearSession();
      showLogin();
    });

    document.getElementById('create-form').addEventListener('submit', (e) => {
      e.preventDefault();
      const name = document.getElementById('create-name').value;
      const currentVersion = document.getElementById('create-version').value;
      const visibleToAll = document.getElementById('create-visible').checked;
      const anonymousAllowed = document.getElementById('create-anonymous').checked;
      createMap(name, currentVersion, visibleToAll, anonymousAllowed).then(() => {
        e.target.reset();
      });
    });

    tabMapsBtn.addEventListener('click', () => showTab('maps'));
    tabUsersBtn.addEventListener('click', () => { showTab('users'); loadUsers(); });

    document.getElementById('create-user-form').addEventListener('submit', (e) => {
      e.preventDefault();
      const payload = {
        username: document.getElementById('cu-username').value,
        cn: document.getElementById('cu-cn').value,
        password: document.getElementById('cu-password').value,
        canCreate: document.getElementById('cu-create').checked,
        canEdit: document.getElementById('cu-edit').checked,
        canDelete: document.getElementById('cu-delete').checked,
        isAdmin: document.getElementById('cu-admin').checked,
      };
      createUser(payload).then(() => {
        e.target.reset();
        document.getElementById('cu-create').checked = true;
        document.getElementById('cu-edit').checked = true;
        document.getElementById('cu-delete').checked = true;
      });
    });

    if (getToken()) {
      showApp();
    } else {
      showLogin();
    }

    fetch('/version').then((res) => res.json()).then((info) => {
      document.getElementById('build-info').textContent =
        ' · ' + (info.version ? info.version + ' · ' : '') + info.commit;
    }).catch(() => {});
