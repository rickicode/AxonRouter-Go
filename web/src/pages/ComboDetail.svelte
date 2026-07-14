<script lang="ts">
import { onMount } from 'svelte';
import { router } from '$lib/router';

onMount(() => {
  router.navigate('/combos');
});
</script>
