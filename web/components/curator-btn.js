(function (global) {
    const CuratorBtn = {
        name: 'CuratorBtn',
        props: {
            // 'default' = accent/green text  'danger' = red text
            // 'indigo'  = indigo text         'muted'  = fg-base (close/defer/cancel)
            variant:     { type: String,  default: 'default' },
            // 'md' (default) | 'sm'
            size:        { type: String,  default: 'md' },
            // stretch to full width
            full:        { type: Boolean, default: false },
            disabled:    { type: Boolean, default: false },
            // show spinner + swap label
            loading:     { type: Boolean, default: false },
            loadingText: { type: String,  default: '' },
            type:        { type: String,  default: 'button' },
        },
        setup(props) {
            const textClass = Vue.computed(() => {
                switch (props.variant) {
                    case 'danger':  return 'text-red-600 dark:text-red-400';
                    case 'indigo':  return 'text-indigo-700 dark:text-indigo-400';
                    case 'muted':   return 'fg-base';
                    default:        return 'fg-accent';
                }
            });

            const sizeClass = Vue.computed(() =>
                props.size === 'sm'
                    ? 'px-3 py-1 text-xs'
                    : 'px-4 py-2 text-sm'
            );

            return { textClass, sizeClass };
        },
        template: `
            <button
                :type="type"
                :disabled="disabled || loading"
                :class="[
                    'bg-raised hover:bg-deep border border-base hover:border-accent',
                    'font-mono font-bold uppercase rounded',
                    'transition-colors duration-150',
                    'disabled:opacity-50 disabled:cursor-not-allowed',
                    'flex items-center justify-center gap-2',
                    textClass,
                    sizeClass,
                    full ? 'w-full' : '',
                ]"
            >
                <span
                    v-if="loading"
                    class="inline-block w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin"
                ></span>
                <template v-if="loading && loadingText">{{ loadingText }}</template>
                <slot v-else />
            </button>
        `,
    };

    global.registerCuratorBtnComponent = function (app) {
        app.component('curator-btn', CuratorBtn);
    };
})(window);
