<template>
  <view class="page">
    <view class="hero">
      <text class="hero__title">千岛小程序</text>
      <text class="hero__subtitle">欢迎使用千岛小程序开发模板</text>
      <view class="hero__status">
        <text
          :class="[
            'hero__status-dot',
            isLoggedIn ? 'hero__status-dot--ok' : '',
          ]"
        />
        <text class="hero__status-text">
          {{ isLoggedIn ? "已登录" : isLoading ? "登录中..." : "未登录" }}
        </text>
      </view>
    </view>

    <view class="content">
      <view
        v-if="!isLoggedIn && !isLoading"
        class="action-btn"
        @tap="handleLogin"
      >
        <text class="action-btn__text">登录</text>
      </view>

      <view v-if="errorMsg" class="error-card">
        <text class="error-card__text">{{ errorMsg }}</text>
      </view>

      <!-- OpenAPI 示例 -->
      <view v-if="isLoggedIn" class="api-demo">
        <!-- 引导态 -->
        <view v-if="!spuData && !spuError" class="guide">
          <view class="guide__icon-wrap">
            <text class="guide__icon">{ }</text>
          </view>
          <text class="guide__title">千岛 OpenAPI</text>
          <text class="guide__desc">
            点击下方按钮，调用岛详情接口获取 SPU 数据
          </text>
          <view class="guide__api-tag">
            <text class="guide__api-method">GET</text>
            <text class="guide__api-path">/spu/v1/detail</text>
          </view>
          <view
            :class="['guide__btn', spuLoading ? 'guide__btn--loading' : '']"
            @tap="fetchSpuDetail"
          >
            <text class="guide__btn-text">{{
              spuLoading ? "请求中..." : "发起请求"
            }}</text>
          </view>
        </view>

        <!-- 成功态 -->
        <view v-if="spuData" class="result">
          <view class="result__status">
            <text class="result__status-dot" />
            <text class="result__status-text">请求成功</text>
            <view class="result__retry" @tap="resetDemo">
              <text class="result__retry-text">重新请求</text>
            </view>
          </view>

          <view class="result__card">
            <image
              v-if="spuData.image"
              class="result__cover"
              :src="spuData.image"
              mode="aspectFit"
            />
            <view class="result__info">
              <view class="result__title-row">
                <text class="result__name">{{ spuData.name }}</text>
                <text class="result__type-tag">{{ spuData.typeName }}</text>
              </view>
              <view class="result__stats">
                <view class="result__stat">
                  <text class="result__stat-num">{{ spuData.wishCount }}</text>
                  <text class="result__stat-label">想要</text>
                </view>
                <view class="result__stat-divider" />
                <view class="result__stat">
                  <text class="result__stat-num">{{ spuData.markCount }}</text>
                  <text class="result__stat-label">拥有</text>
                </view>
                <view class="result__stat-divider" />
                <view class="result__stat">
                  <text class="result__stat-num">{{
                    spuData.wishCount3day
                  }}</text>
                  <text class="result__stat-label">3日想要</text>
                </view>
                <view class="result__stat-divider" />
                <view class="result__stat">
                  <text class="result__stat-num">{{
                    spuData.markCount3day
                  }}</text>
                  <text class="result__stat-label">3日拥有</text>
                </view>
              </view>
            </view>
          </view>

          <view
            v-if="
              spuData.entryProfileItems && spuData.entryProfileItems.length
            "
            class="result__detail-card"
          >
            <text class="result__detail-title">详细信息</text>
            <view
              v-for="item in spuData.entryProfileItems"
              :key="item.id"
              class="result__prop"
            >
              <text class="result__prop-label">{{ item.propertyName }}</text>
              <text class="result__prop-value">{{ item.dataValue || "-" }}</text>
            </view>
          </view>
        </view>

        <!-- 错误态 -->
        <view v-if="spuError" class="error-state">
          <text class="error-state__title">请求失败</text>
          <text class="error-state__msg">{{ spuError }}</text>
          <view class="guide__btn" @tap="resetDemo">
            <text class="guide__btn-text">重试</text>
          </view>
        </view>
      </view>
    </view>
  </view>
</template>

<script>
import { ref, onMounted } from "vue";
import { doLogin, clearTokens } from "../../apis/auth";
import { getSpuDetail } from "../../apis/spu";
import "./index.scss";

export default {
  setup() {
    const isLoggedIn = ref(false);
    const isLoading = ref(false);
    const errorMsg = ref("");

    const spuData = ref(null);
    const spuLoading = ref(false);
    const spuError = ref("");

    async function handleLogin() {
      isLoading.value = true;
      errorMsg.value = "";
      try {
        await doLogin();
        isLoggedIn.value = true;
      } catch (err) {
        errorMsg.value = err?.message || err?.errMsg || JSON.stringify(err);
      } finally {
        isLoading.value = false;
      }
    }

    async function fetchSpuDetail() {
      if (spuLoading.value) return;
      spuLoading.value = true;
      spuError.value = "";
      spuData.value = null;
      try {
        spuData.value = await getSpuDetail("978074656223267833");
      } catch (err) {
        spuError.value = err?.message || err?.errMsg || JSON.stringify(err);
      } finally {
        spuLoading.value = false;
      }
    }

    function resetDemo() {
      spuData.value = null;
      spuError.value = "";
    }

    onMounted(() => {
      clearTokens();
    });

    return {
      isLoggedIn,
      isLoading,
      errorMsg,
      handleLogin,
      spuData,
      spuLoading,
      spuError,
      fetchSpuDetail,
      resetDemo,
    };
  },
};
</script>
